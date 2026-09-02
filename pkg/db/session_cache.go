package db

import (
	"context"
	"errors"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/maypok86/otter/v2"
)

const (
	sessionCacheCapacity     = 10_000
	sessionCacheResidencyTTL = 15 * time.Minute
	sessionValidationLease   = 10 * time.Minute
)

var ErrStaleSessionRead = errors.New("stale session authority read")

// SessionCache stores process-local session Authority and Payload.
type SessionCache struct {
	*otter.Cache[string, *session.Session]
}

func newSessionCache(clock otter.Clock) *SessionCache {
	return &SessionCache{otter.Must(&otter.Options[string, *session.Session]{
		MaximumSize:      sessionCacheCapacity,
		InitialCapacity:  100,
		ExpiryCalculator: otter.ExpiryAccessing[string, *session.Session](sessionCacheResidencyTTL),
		Clock:            clock,
		Logger:           &pcOtterLogger{},
	})}
}

// Resolve returns a cached session or refreshes it from PostgreSQL.
func (ss *SessionStore) Resolve(ctx context.Context, sid string) (*session.Session, error) {
	return ss.resolve(ctx, sid, time.Now())
}

// resolve uses an explicit read time for deterministic lease checks.
func (ss *SessionStore) resolve(ctx context.Context, sid string, readTime time.Time) (*session.Session, error) {
	observed, observedFound := ss.sessionCache.GetIfPresent(sid)
	if observedFound {
		if observed.IsRevoked() {
			return nil, session.ErrSessionMissing
		}
		if isUsableCachedSession(observed, readTime) {
			return observed, nil
		}
	}

	stored, err := ss.store.Impl().RetrieveLiveSession(ctx, sid)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			ss.evictObservedSession(sid, observed, observedFound)
			return nil, session.ErrSessionMissing
		}
		return nil, err
	}

	candidate, err := ss.sessionFromStored(stored, readTime)
	if err != nil {
		ss.evictObservedSession(sid, observed, observedFound)
		return nil, err
	}

	actual, _ := ss.sessionCache.Compute(sid, func(current *session.Session, found bool) (*session.Session, otter.ComputeOp) {
		if !found {
			if observedFound {
				return nil, otter.CancelOp
			}
			return candidate, otter.WriteOp
		}

		currentAuthority, currentOK := current.Authority()
		candidateAuthority, _ := candidate.Authority()
		if currentOK && currentAuthority.IsRevoked() {
			return current, otter.CancelOp
		}
		if !currentOK || candidateAuthority.Version > currentAuthority.Version {
			return candidate, otter.WriteOp
		}
		if candidateAuthority.Version == currentAuthority.Version {
			if !observedFound || current != observed {
				return current, otter.CancelOp
			}
			return session.NewSessionWithAuthority(candidateAuthority, current.Payload()), otter.WriteOp
		}
		return current, otter.CancelOp
	})

	if actual == nil {
		return nil, ErrStaleSessionRead
	}
	if !isUsableCachedSession(actual, readTime) {
		return nil, ErrStaleSessionRead
	}
	return actual, nil
}

// sessionFromStored converts PostgreSQL state into a cacheable session.
func (ss *SessionStore) sessionFromStored(stored *session.StoredSession, readTime time.Time) (*session.Session, error) {
	payload := session.NewPayload(stored.SessionID, ss)
	if err := payload.Replace(stored.Payload); err != nil {
		return nil, err
	}

	return session.NewSessionWithAuthority(authorityFromStored(stored, readTime), payload), nil
}

func authorityFromStored(stored *session.StoredSession, readTime time.Time) session.Authority {
	leaseUntil := time.Time{}
	if stored.State == session.StateAuthenticated {
		leaseUntil = readTime.Add(sessionValidationLease)
		if stored.ExpiresAt.Before(leaseUntil) {
			leaseUntil = stored.ExpiresAt
		}
	}
	return session.Authority{
		State:          stored.State,
		Version:        stored.Version,
		UserID:         stored.UserID,
		ChallengeKind:  stored.ChallengeKind,
		ChallengeEmail: stored.ChallengeEmail,
		ExpiresAt:      stored.ExpiresAt,
		LeaseUntil:     leaseUntil,
	}
}

func isUsableCachedSession(sess *session.Session, now time.Time) bool {
	authority, ok := sess.Authority()
	if !ok {
		return true
	}
	if !now.Before(authority.ExpiresAt) {
		return false
	}
	return authority.State == session.StatePending ||
		(authority.State == session.StateAuthenticated && now.Before(authority.LeaseUntil))
}

// evictObservedSession removes only the entry seen before an authoritative read.
func (ss *SessionStore) evictObservedSession(sid string, observed *session.Session, observedFound bool) {
	if !observedFound {
		return
	}
	ss.sessionCache.ComputeIfPresent(sid, func(current *session.Session) (*session.Session, otter.ComputeOp) {
		if current == observed {
			return nil, otter.InvalidateOp
		}
		return current, otter.CancelOp
	})
}
