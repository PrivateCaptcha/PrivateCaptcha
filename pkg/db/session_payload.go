package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/maypok86/otter/v2"
)

// UpdatePayload queues a persisted Payload after its in-memory mutation.
func (ss *SessionStore) UpdatePayload(ctx context.Context, sid string) {
	if cached, ok := ss.sessionCache.GetIfPresent(sid); ok {
		if _, hasAuthority := cached.Authority(); !hasAuthority {
			return
		}
	}
	ss.payloadMu.RLock()
	defer ss.payloadMu.RUnlock()
	if ss.payloadStopped {
		ss.dropPayload(ctx, sid, "shutdown", nil)
		return
	}

	select {
	case ss.payloadChan <- sid:
	default:
		ss.dropPayload(ctx, sid, "queue full", nil)
	}
}

func (ss *SessionStore) persistPayloads(ctx context.Context, batch map[string]uint) error {
	updates := make([]session.PayloadUpdate, 0, len(batch))
	payloads := make(map[string]*session.Payload, len(batch))
	now := time.Now()
	for sid := range batch {
		cached, ok := ss.sessionCache.GetIfPresent(sid)
		if !ok {
			ss.dropPayload(ctx, sid, "cache eviction", nil)
			continue
		}

		authority, ok := cached.Authority()
		if !ok || (authority.State != session.StatePending && authority.State != session.StateAuthenticated) || !now.Before(authority.ExpiresAt) {
			ss.dropPayload(ctx, sid, "session is not persistable", nil)
			continue
		}
		currentPayload := cached.Payload()
		payload, err := currentPayload.Snapshot()
		if err != nil {
			ss.dropPayload(ctx, sid, "serialization", err)
			continue
		}
		updates = append(updates, session.PayloadUpdate{
			SessionID:       sid,
			ExpectedVersion: authority.Version,
			Payload:         payload,
		})
		payloads[sid] = currentPayload
	}

	if len(updates) == 0 {
		return nil
	}
	results, err := ss.store.Impl().UpdateSessionPayloads(ctx, updates)
	if err != nil {
		return err
	}

	versions := make(map[string]int32, len(results))
	for _, result := range results {
		versions[result.SessionID] = result.Version
	}
	for _, update := range updates {
		if version, ok := versions[update.SessionID]; ok {
			ss.publishPayloadVersion(update.SessionID, update.ExpectedVersion, version, payloads[update.SessionID])
		} else {
			ss.evictPayloadVersion(update.SessionID, update.ExpectedVersion)
		}
	}
	return nil
}

func (ss *SessionStore) publishPayloadVersion(sid string, expectedVersion, version int32, payload *session.Payload) {
	ss.sessionCache.ComputeIfPresent(sid, func(current *session.Session) (*session.Session, otter.ComputeOp) {
		if current.IsRevoked() {
			return current, otter.CancelOp
		}
		authority, ok := current.Authority()
		if !ok || authority.Version != expectedVersion {
			return current, otter.CancelOp
		}
		if current.Payload() != payload {
			return nil, otter.InvalidateOp
		}
		authority.Version = version
		return session.NewSessionWithAuthority(authority, current.Payload()), otter.WriteOp
	})
}

func (ss *SessionStore) evictPayloadVersion(sid string, expectedVersion int32) {
	ss.sessionCache.ComputeIfPresent(sid, func(current *session.Session) (*session.Session, otter.ComputeOp) {
		if current.IsRevoked() {
			return current, otter.CancelOp
		}
		authority, ok := current.Authority()
		if ok && authority.Version == expectedVersion {
			return nil, otter.InvalidateOp
		}
		return current, otter.CancelOp
	})
}

func (ss *SessionStore) dropPayload(ctx context.Context, sid, reason string, err error) {
	attrs := []any{common.SessionHashAttr(common.HashSessionID(sid)), "reason", reason}
	if err != nil {
		attrs = append(attrs, common.ErrAttr(err))
	}
	slog.WarnContext(ctx, "Dropping session Payload persistence event", attrs...)
	ss.metrics.ObserveEventDropped(common.SessionEventType)
}

func (ss *SessionStore) dropQueuedPayloads(ctx context.Context) {
	for {
		select {
		case sid, ok := <-ss.payloadChan:
			if !ok {
				return
			}
			ss.dropPayload(ctx, sid, "shutdown", nil)
		default:
			return
		}
	}
}
