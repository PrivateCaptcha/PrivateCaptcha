package db

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/maypok86/otter/v2"
)

const (
	sessionBatchSize = 20
	sessionCacheTTL  = 3 * time.Hour
)

type SessionStore struct {
	store          Implementor
	sessionCache   *SessionCache
	payloadMu      sync.RWMutex
	payloadChan    chan string
	payloadStopped bool
	renewalMu      sync.RWMutex
	renewalChan    chan string
	renewalStopped bool
	batchSize      int
	processCancel  context.CancelFunc
	metrics        common.BaseMetrics
}

func NewSessionStore(store Implementor, metrics common.BaseMetrics) *SessionStore {
	return &SessionStore{
		store:         store,
		sessionCache:  newSessionCache(nil),
		payloadChan:   make(chan string, sessionBatchSize),
		renewalChan:   make(chan string, sessionBatchSize),
		batchSize:     sessionBatchSize,
		metrics:       metrics,
		processCancel: func() {},
	}
}

func (ss *SessionStore) Start(ctx context.Context, interval time.Duration) {
	var cancelCtx context.Context
	cancelCtx, ss.processCancel = context.WithCancel(
		context.WithValue(ctx, common.TraceIDContextKey, "persist_session"))
	go common.ProcessBatchMap(cancelCtx, ss.payloadChan, interval, ss.batchSize, ss.batchSize*100, ss.persistPayloads)
	go common.ProcessBatchMap(cancelCtx, ss.renewalChan, interval, ss.batchSize, ss.batchSize*100, ss.renewSessionExpirations)
}

var _ session.Store = (*SessionStore)(nil)
var _ session.PayloadStore = (*SessionStore)(nil)

// StartAnonymousSession creates a process-local session that can resolve without PostgreSQL.
func (ss *SessionStore) StartAnonymousSession(sid string) *session.Session {
	sess := session.NewAnonymousSession(sid, ss)
	ss.sessionCache.Set(sid, sess)
	return sess
}

// publishTransitionSession installs SQL-returned Authority without regressing cached state.
func (ss *SessionStore) publishTransitionSession(stored *session.StoredSession) (*session.Session, session.Authority, error) {
	if stored == nil {
		return nil, session.Authority{}, session.ErrInvalidTransitionResult
	}
	readTime := time.Now()
	candidateAuthority := authorityFromStored(stored, readTime)
	if candidateAuthority.IsRevoked() {
		published, err := ss.publishRevocation(stored.SessionID, candidateAuthority)
		return published, candidateAuthority, err
	}
	candidate, err := ss.sessionFromStored(stored, readTime)
	if err != nil {
		return nil, session.Authority{}, err
	}
	published, _ := ss.sessionCache.Compute(stored.SessionID, func(current *session.Session, found bool) (*session.Session, otter.ComputeOp) {
		if !found {
			return candidate, otter.WriteOp
		}
		currentAuthority, hasCurrentAuthority := current.Authority()
		if hasCurrentAuthority && currentAuthority.IsRevoked() {
			return current, otter.CancelOp
		}
		if hasCurrentAuthority && currentAuthority.Version > candidateAuthority.Version {
			return current, otter.CancelOp
		}
		return session.NewSessionWithAuthority(candidateAuthority, current.Payload()), otter.WriteOp
	})
	if published.IsRevoked() {
		return published, candidateAuthority, ErrStaleSessionRead
	}
	return published, candidateAuthority, nil
}

// publishRevocation retains terminal Authority so delayed live results cannot replace it.
func (ss *SessionStore) publishRevocation(sid string, authority session.Authority) (*session.Session, error) {
	if sid == "" || !authority.IsRevoked() {
		return nil, session.ErrInvalidTransitionResult
	}
	published, _ := ss.sessionCache.Compute(sid, func(current *session.Session, found bool) (*session.Session, otter.ComputeOp) {
		if found {
			currentAuthority, ok := current.Authority()
			if ok && currentAuthority.IsRevoked() && currentAuthority.Version > authority.Version {
				return current, otter.CancelOp
			}
			return session.NewSessionWithAuthority(authority, current.Payload()), otter.WriteOp
		}
		return session.NewSessionWithAuthority(authority, session.NewPayload(sid, ss)), otter.WriteOp
	})
	return published, nil
}

// publishConsumedPredecessor retains a committed revocation before processing its successor.
func (ss *SessionStore) publishConsumedPredecessor(sid string, stored *session.StoredSession) error {
	if stored == nil || stored.SessionID != sid || stored.State != session.StateRevoked {
		return session.ErrInvalidTransitionResult
	}
	_, _, err := ss.publishTransitionSession(stored)
	return err
}

// challengeResult converts a shared challenge result and publishes its returned session.
func (ss *SessionStore) challengeResult(result *session.ChallengeIssueResult) (*session.ChallengeResult, error) {
	if result == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	if result.Outcome == session.TransitionSucceeded && result.Session == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	converted := &session.ChallengeResult{Outcome: result.Outcome}
	if result.Session != nil {
		published, authority, err := ss.publishTransitionSession(result.Session)
		if err != nil {
			return nil, err
		}
		converted.Session = published
		converted.Authority = authority
	}
	return converted, nil
}

// IssueSignInChallenge commits an anonymous session as a pending sign-in challenge.
func (ss *SessionStore) IssueSignInChallenge(ctx context.Context, issue session.SignInChallengeIssue) (*session.ChallengeResult, error) {
	result, err := ss.store.Impl().IssueSignInChallenge(ctx, &dbgen.IssueSignInChallengeParams{
		SessionID: issue.SessionID, UserID: issue.UserID, ChallengeCode: Text(issue.ChallengeCode),
		Data: issue.Payload, SessionTtl: issue.SessionTTL, ChallengeTtl: issue.ChallengeTTL, MaxAttempts: issue.MaxAttempts,
	})
	if err != nil {
		return nil, err
	}
	return ss.challengeResult(result)
}

// IssueRegistrationChallenge commits an anonymous session as a pending registration challenge.
func (ss *SessionStore) IssueRegistrationChallenge(ctx context.Context, issue session.RegistrationChallengeIssue) (*session.ChallengeResult, error) {
	inviteID := pgtype.Int4{}
	if issue.InviteID > 0 {
		inviteID = Int(issue.InviteID)
	}
	result, err := ss.store.Impl().IssueRegistrationChallenge(ctx, &dbgen.IssueRegistrationChallengeParams{
		SessionID: issue.SessionID, ChallengeEmail: Text(issue.ChallengeEmail), ChallengeCode: Text(issue.ChallengeCode),
		Data: issue.Payload, RequiresVerification: Bool(issue.RequiresVerification), InviteID: inviteID,
		SessionTtl: issue.SessionTTL, ChallengeTtl: issue.ChallengeTTL, MaxAttempts: issue.MaxAttempts,
	})
	if err != nil {
		return nil, err
	}
	return ss.challengeResult(result)
}

// ResendPendingChallenge replaces the code and expiration on a live pending challenge.
func (ss *SessionStore) ResendPendingChallenge(ctx context.Context, resend session.PendingChallengeResend) (*session.ChallengeResult, error) {
	result, err := ss.store.Impl().ResendPendingChallenge(ctx, &dbgen.ResendPendingChallengeParams{
		SessionID: resend.SessionID, ChallengeCode: Text(resend.ChallengeCode),
		ChallengeTtl: resend.ChallengeTTL, MaxAttempts: resend.MaxAttempts,
	})
	if err != nil {
		return nil, err
	}
	return ss.challengeResult(result)
}

// ConsumeSignInChallenge verifies the code and publishes the authenticated successor on success.
func (ss *SessionStore) ConsumeSignInChallenge(ctx context.Context, consume session.SignInChallengeConsume) (*session.ChallengeResult, error) {
	result, err := ss.store.Impl().ConsumeSignInChallenge(ctx, &dbgen.ConsumeSignInChallengeParams{
		SessionID: consume.SessionID, SuccessorSessionID: consume.SuccessorSessionID,
		ChallengeCode: Text(consume.ChallengeCode), SuccessorData: consume.SuccessorPayload,
		SuccessorTtl: consume.SuccessorTTL, MaxAttempts: consume.MaxAttempts,
	})
	// Keep the committed predecessor revocation even when the successor result is malformed.
	if result != nil && result.Outcome == session.TransitionSucceeded {
		if publishErr := ss.publishConsumedPredecessor(consume.SessionID, result.Session); publishErr != nil {
			return nil, publishErr
		}
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	converted := &session.ChallengeResult{Outcome: result.Outcome, FailedAttempts: result.FailedAttempts}
	if result.Outcome == session.TransitionSucceeded {
		if result.Successor == nil {
			return nil, session.ErrInvalidTransitionResult
		}
		converted.Session, converted.Authority, err = ss.publishTransitionSession(result.Successor)
	} else if result.Session != nil {
		converted.Session, converted.Authority, err = ss.publishTransitionSession(result.Session)
	}
	return converted, err
}

// ConsumeRegistrationChallenge verifies the code and returns authoritative registration data.
func (ss *SessionStore) ConsumeRegistrationChallenge(ctx context.Context, consume session.RegistrationChallengeConsume) (*session.RegistrationConsumeResult, error) {
	result, err := ss.store.Impl().ConsumeRegistrationChallenge(ctx, &dbgen.ConsumeRegistrationChallengeParams{
		SessionID: consume.SessionID, ChallengeCode: Text(consume.ChallengeCode), MaxAttempts: consume.MaxAttempts,
	})
	if result != nil && result.Outcome == session.TransitionSucceeded {
		if publishErr := ss.publishConsumedPredecessor(consume.SessionID, result.Session); publishErr != nil {
			return nil, publishErr
		}
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	if result.Outcome != session.TransitionSucceeded && result.Session != nil {
		if _, _, err := ss.publishTransitionSession(result.Session); err != nil {
			return nil, err
		}
	}
	return &session.RegistrationConsumeResult{
		Outcome: result.Outcome, FailedAttempts: result.FailedAttempts,
		Email: result.Email, Name: result.Name, InviteID: result.InviteID,
	}, nil
}

// CreateRegistrationSuccessor publishes an authenticated session after account creation.
func (ss *SessionStore) CreateRegistrationSuccessor(ctx context.Context, create session.RegistrationSuccessorCreate) (*session.ChallengeResult, error) {
	result, err := ss.store.Impl().CreateRegistrationSuccessor(ctx, &dbgen.CreateRegistrationSuccessorParams{
		SessionID: create.SessionID, UserID: create.UserID, Data: create.Payload, SessionTtl: create.TTL,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	if result.Outcome == session.TransitionSucceeded && result.Session == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	converted := &session.ChallengeResult{Outcome: result.Outcome}
	if result.Session != nil {
		converted.Session, converted.Authority, err = ss.publishTransitionSession(result.Session)
	}
	return converted, err
}

// IssueEmailChangeChallenge adds a verification challenge to an authenticated session.
func (ss *SessionStore) IssueEmailChangeChallenge(ctx context.Context, issue session.EmailChangeChallengeIssue) (*session.ChallengeResult, error) {
	result, err := ss.store.Impl().IssueEmailChangeChallenge(ctx, &dbgen.IssueEmailChangeChallengeParams{
		SessionID: issue.SessionID, ChallengeCode: Text(issue.ChallengeCode), ChallengeTtl: issue.ChallengeTTL,
	})
	if err != nil {
		return nil, err
	}
	return ss.challengeResult(result)
}

// ConsumeEmailChangeChallenge verifies and clears an authenticated session's email challenge.
func (ss *SessionStore) ConsumeEmailChangeChallenge(ctx context.Context, consume session.EmailChangeChallengeConsume) (*session.ChallengeResult, error) {
	result, err := ss.store.Impl().ConsumeEmailChangeChallenge(ctx, &dbgen.ConsumeEmailChangeChallengeParams{
		SessionID: consume.SessionID, ChallengeCode: Text(consume.ChallengeCode), MaxAttempts: consume.MaxAttempts,
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	if result.Outcome == session.TransitionSucceeded && result.Session == nil {
		return nil, session.ErrInvalidTransitionResult
	}
	converted := &session.ChallengeResult{Outcome: result.Outcome, FailedAttempts: result.FailedAttempts}
	if result.Session != nil {
		converted.Session, converted.Authority, err = ss.publishTransitionSession(result.Session)
	}
	return converted, err
}

// RevokeSession records shared revocation before publishing it to the local cache.
func (ss *SessionStore) RevokeSession(ctx context.Context, sid string) (*session.RevocationResult, error) {
	result, err := ss.store.Impl().RevokeSession(ctx, sid)
	if err != nil {
		return nil, err
	}
	if result == nil {
		// SQL has no version to publish, but must not erase a terminal observation.
		ss.sessionCache.ComputeIfPresent(sid, func(current *session.Session) (*session.Session, otter.ComputeOp) {
			if current.IsRevoked() {
				return current, otter.CancelOp
			}
			return nil, otter.InvalidateOp
		})
		return nil, nil
	}
	if result.SessionID != sid {
		return nil, session.ErrInvalidTransitionResult
	}
	if _, err := ss.publishRevocation(result.SessionID, session.Authority{
		State: result.State, Version: result.Version, UserID: result.UserID,
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// RevokeUserSessions revokes every user-bound session and publishes each returned SID.
func (ss *SessionStore) RevokeUserSessions(ctx context.Context, userID int32) error {
	results, err := ss.store.Impl().RevokeUserSessions(ctx, userID)
	if err != nil {
		return err
	}
	for _, result := range results {
		if _, err := ss.publishRevocation(result.SessionID, session.Authority{
			State: result.State, Version: result.Version, UserID: userID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (ss *SessionStore) Stop() {
	ss.payloadMu.Lock()
	ss.payloadStopped = true
	ss.payloadMu.Unlock()
	ss.renewalMu.Lock()
	ss.renewalStopped = true
	ss.renewalMu.Unlock()
	ss.processCancel()
	ss.dropQueuedPayloads(context.Background())
	ss.dropQueuedExpirationRenewals(context.Background())
}

func (ss *SessionStore) Shutdown() {
	slog.Debug("Shutting down persisting sessions")
	ss.payloadMu.Lock()
	ss.payloadStopped = true
	close(ss.payloadChan)
	ss.payloadMu.Unlock()
	ss.renewalMu.Lock()
	ss.renewalStopped = true
	close(ss.renewalChan)
	ss.renewalMu.Unlock()
	ss.dropQueuedPayloads(context.Background())
	ss.dropQueuedExpirationRenewals(context.Background())
}

// EnqueueExpirationRenewal queues best-effort persistence without request backpressure.
func (ss *SessionStore) EnqueueExpirationRenewal(ctx context.Context, sid string) {
	ss.renewalMu.RLock()
	defer ss.renewalMu.RUnlock()
	if ss.renewalStopped {
		ss.dropExpirationRenewal(ctx, sid, "shutdown")
		return
	}

	select {
	case ss.renewalChan <- sid:
	default:
		ss.dropExpirationRenewal(ctx, sid, "queue full")
	}
}

func (ss *SessionStore) renewSessionExpirations(ctx context.Context, batch map[string]uint) error {
	sids := make([]string, 0, len(batch))
	for sid := range batch {
		sids = append(sids, sid)
	}
	results, err := ss.store.Impl().RenewSessionExpirations(ctx, sids, sessionCacheTTL)
	if err != nil {
		return err
	}

	returned := make(map[string]session.ExpirationRenewalResult, len(results))
	for _, result := range results {
		returned[result.SessionID] = result
	}
	for sid := range batch {
		result, ok := returned[sid]
		if !ok {
			ss.evictExpirationRenewal(sid)
			continue
		}
		ss.publishExpirationRenewal(result)
	}
	return nil
}

func (ss *SessionStore) publishExpirationRenewal(result session.ExpirationRenewalResult) {
	ss.sessionCache.ComputeIfPresent(result.SessionID, func(current *session.Session) (*session.Session, otter.ComputeOp) {
		if current.IsRevoked() {
			return current, otter.CancelOp
		}
		authority, ok := current.Authority()
		if !ok || authority.Version != result.Version {
			return nil, otter.InvalidateOp
		}
		authority.ExpiresAt = result.ExpiresAt
		return session.NewSessionWithAuthority(authority, current.Payload()), otter.WriteOp
	})
}

func (ss *SessionStore) evictExpirationRenewal(sid string) {
	ss.sessionCache.ComputeIfPresent(sid, func(current *session.Session) (*session.Session, otter.ComputeOp) {
		if current.IsRevoked() {
			return current, otter.CancelOp
		}
		return nil, otter.InvalidateOp
	})
}

func (ss *SessionStore) dropExpirationRenewal(ctx context.Context, sid, reason string) {
	slog.WarnContext(ctx, "Dropping session expiration renewal event", common.SessionHashAttr(common.HashSessionID(sid)), "reason", reason)
	ss.metrics.ObserveEventDropped(common.SessionEventType)
}

func (ss *SessionStore) dropQueuedExpirationRenewals(ctx context.Context) {
	for {
		select {
		case sid, ok := <-ss.renewalChan:
			if !ok {
				return
			}
			ss.dropExpirationRenewal(ctx, sid, "shutdown")
		default:
			return
		}
	}
}

func (ss *SessionStore) TTL() time.Duration {
	return sessionCacheTTL
}
