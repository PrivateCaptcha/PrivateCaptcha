package db

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/maypok86/otter/v2"
	"golang.org/x/sync/singleflight"
)

const (
	sessionBatchSize                  = 20
	sessionCacheTTL                   = 3 * time.Hour
	sessionValidationLease            = 10 * time.Minute
	sessionExpirationRenewalThreshold = 2*time.Hour + 30*time.Minute
	sessionBackpressureTimeout        = 200 * time.Millisecond
	sessionCleanupTimeout             = 2 * time.Second
)

type SessionStore struct {
	store            Implementor
	persistDelayChan chan string
	expirationChan   chan string
	batchSize        int
	processCancel    context.CancelFunc
	persistKey       session.SessionKey
	metrics          common.BaseMetrics
	persistMu        sync.Mutex
	pendingDeletes   map[string]struct{}
	now              func() time.Time
	validations      singleflight.Group
}

func NewSessionStore(store Implementor, persistKey session.SessionKey, metrics common.BaseMetrics) *SessionStore {
	return &SessionStore{
		store:            store,
		persistDelayChan: make(chan string, sessionBatchSize),
		expirationChan:   make(chan string, sessionBatchSize),
		batchSize:        sessionBatchSize,
		persistKey:       persistKey,
		metrics:          metrics,
		processCancel:    func() {},
		pendingDeletes:   make(map[string]struct{}),
		now:              time.Now,
	}
}

func (ss *SessionStore) Start(ctx context.Context, interval time.Duration) {
	var cancelCtx context.Context
	cancelCtx, ss.processCancel = context.WithCancel(
		context.WithValue(ctx, common.TraceIDContextKey, "persist_session"))
	go common.ProcessBatchMap(cancelCtx, ss.persistDelayChan, interval, ss.batchSize, ss.batchSize*100, ss.persistSessions)
	go common.ProcessBatchMap(cancelCtx, ss.expirationChan, interval, ss.batchSize, ss.batchSize*100, ss.heartbeatSessions)
}

var _ session.Store = (*SessionStore)(nil)

func (ss *SessionStore) Stop() {
	ss.processCancel()
}

func (ss *SessionStore) Shutdown() {
	slog.Debug("Shutting down persisting sessions")
	close(ss.persistDelayChan)
	close(ss.expirationChan)
}

// Init registers a local-only session without creating PostgreSQL authority.
func (ss *SessionStore) Init(ctx context.Context, session *session.Session) error {
	return ss.store.Impl().CacheUserSession(ctx, session.Data())
}

// Create synchronously turns a cached session into a persistent pending session.
func (ss *SessionStore) Create(ctx context.Context, sess *session.Session) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	return ss.create(ctx, sess, session.StatePending)
}

// CreateSignInChallenge persists a user-bound pending challenge before its cookie is committed.
func (ss *SessionStore) CreateSignInChallenge(ctx context.Context, sess *session.Session, encodedCode, email string, challengeTTL time.Duration) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil {
		return ErrInvalidInput
	}
	impl := ss.store.Impl()
	cached, err := impl.GetCachedUserSession(ctx, sess.ID())
	if err != nil || cached != sess.Data() {
		return session.ErrSessionMissing
	}
	userID, ok := sess.Get(ctx, session.KeyUserID).(int32)
	if !ok {
		return ErrInvalidInput
	}
	if err := impl.CreateUserSignInChallenge(ctx, sess.Data(), ss.persistKey, userID, encodedCode, email, sessionCacheTTL, challengeTTL); err != nil {
		return err
	}
	sess.Data().MarkValidated(ss.now())
	return nil
}

// CreateRegistrationChallenge persists a pending registration challenge before its cookie is committed.
func (ss *SessionStore) CreateRegistrationChallenge(ctx context.Context, sess *session.Session, encodedCode, email string, challengeTTL time.Duration) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil {
		return ErrInvalidInput
	}
	impl := ss.store.Impl()
	cached, err := impl.GetCachedUserSession(ctx, sess.ID())
	if err != nil || cached != sess.Data() {
		return session.ErrSessionMissing
	}
	if err := impl.CreateUserRegistrationChallenge(ctx, sess.Data(), ss.persistKey, encodedCode, email, sessionCacheTTL, challengeTTL); err != nil {
		return err
	}
	sess.Data().MarkValidated(ss.now())
	return nil
}

// ConsumeSignInChallenge atomically rotates a valid challenge; invalid codes only increment shared attempts.
func (ss *SessionStore) ConsumeSignInChallenge(ctx context.Context, current, successor *session.Session, encodedCode string, maxFailedAttempts int32) (session.SignInChallengeResult, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if current == nil || successor == nil || current.ID() == successor.ID() || maxFailedAttempts <= 0 {
		return session.SignInChallengeResult{}, ErrInvalidInput
	}
	expectedUserID, ok := current.Get(ctx, session.KeyUserID).(int32)
	if !ok || expectedUserID <= 0 {
		return session.SignInChallengeResult{}, ErrInvalidInput
	}
	impl := ss.store.Impl()
	result, err := impl.ConsumeUserSignInChallenge(ctx, current.ID(), successor.Data(), ss.persistKey, expectedUserID, encodedCode, maxFailedAttempts, sessionCacheTTL)
	if err != nil || !result.Consumed {
		return result, err
	}
	successor.Data().MarkValidated(ss.now())
	if err := impl.CacheUserSession(ctx, successor.Data()); err != nil {
		slog.WarnContext(ctx, "Failed to cache consumed sign-in successor", common.ErrAttr(err))
	}
	current.Data().MarkStale()
	impl.DeleteCachedUserSession(ctx, current.ID())
	return result, nil
}

// ConsumeRegistrationChallenge creates one processing successor, or leaves a verification-only challenge pending.
func (ss *SessionStore) ConsumeRegistrationChallenge(ctx context.Context, current, successor *session.Session, encodedCode string, maxFailedAttempts int32, allowConsumption bool) (session.RegistrationChallengeResult, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if current == nil || successor == nil || current.ID() == successor.ID() || maxFailedAttempts <= 0 {
		return session.RegistrationChallengeResult{}, ErrInvalidInput
	}
	impl := ss.store.Impl()
	result, err := impl.ConsumeUserRegistrationChallenge(ctx, current.ID(), successor.Data(), ss.persistKey, encodedCode, maxFailedAttempts, allowConsumption, sessionCacheTTL)
	if err != nil || !result.Consumed {
		return result, err
	}
	if err := impl.CacheUserSession(ctx, successor.Data()); err != nil {
		slog.WarnContext(ctx, "Failed to cache registration processing successor", common.ErrAttr(err))
	}
	current.Data().MarkStale()
	impl.DeleteCachedUserSession(ctx, current.ID())
	return result, nil
}

// ReissueSignInChallenge replaces the shared code and resets its attempt count.
func (ss *SessionStore) ReissueSignInChallenge(ctx context.Context, sess *session.Session, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (session.SignInChallengeReissue, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil {
		return session.SignInChallengeReissue{}, ErrInvalidInput
	}
	return ss.store.Impl().ReissueUserSignInChallenge(ctx, sess.Data(), encodedCode, fallbackEncodedCode, challengeTTL)
}

// ReissueRegistrationChallenge replaces the shared code and resets its attempt count.
func (ss *SessionStore) ReissueRegistrationChallenge(ctx context.Context, sess *session.Session, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (session.RegistrationChallengeReissue, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil {
		return session.RegistrationChallengeReissue{}, ErrInvalidInput
	}
	return ss.store.Impl().ReissueUserRegistrationChallenge(ctx, sess.Data(), encodedCode, fallbackEncodedCode, challengeTTL)
}

// IssueEmailChangeChallenge adds a version-guarded challenge to a live authenticated session.
func (ss *SessionStore) IssueEmailChangeChallenge(ctx context.Context, sess *session.Session, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (session.EmailChangeChallengeIssue, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil {
		return session.EmailChangeChallengeIssue{}, ErrInvalidInput
	}
	expectedUserID, ok := sess.Get(ctx, session.KeyUserID).(int32)
	if !ok || expectedUserID <= 0 {
		return session.EmailChangeChallengeIssue{}, ErrInvalidInput
	}
	issued, err := ss.store.Impl().IssueUserEmailChangeChallenge(ctx, sess.Data(), expectedUserID, encodedCode, fallbackEncodedCode, challengeTTL)
	if err != nil || issued.EncodedCode != "" {
		return issued, err
	}
	sess.Data().MarkStale()
	ss.store.Impl().DeleteCachedUserSession(ctx, sess.ID())
	return session.EmailChangeChallengeIssue{}, session.ErrSessionMissing
}

// ConsumeEmailChangeChallenge atomically updates the email, or records the failed shared attempt.
func (ss *SessionStore) ConsumeEmailChangeChallenge(ctx context.Context, sess *session.Session, newEmail, encodedCode string, maxFailedAttempts int32) (session.EmailChangeChallengeResult, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil || newEmail == "" || maxFailedAttempts <= 0 {
		return session.EmailChangeChallengeResult{}, ErrInvalidInput
	}
	expectedUserID, ok := sess.Get(ctx, session.KeyUserID).(int32)
	if !ok || expectedUserID <= 0 {
		return session.EmailChangeChallengeResult{}, ErrInvalidInput
	}
	impl := ss.store.Impl()
	result, err := impl.ConsumeUserEmailChangeChallenge(ctx, sess.Data(), expectedUserID, newEmail, encodedCode, maxFailedAttempts)
	if err != nil {
		return session.EmailChangeChallengeResult{}, err
	}
	if result.Consumed {
		sess.Data().MarkValidated(ss.now())
	}
	if err := impl.CacheUserSession(ctx, sess.Data()); err != nil {
		slog.WarnContext(ctx, "Failed to cache email challenge session", common.ErrAttr(err))
	}
	return result, nil
}

// FinalizeRegistration promotes the matching processing successor after account creation.
func (ss *SessionStore) FinalizeRegistration(ctx context.Context, sess *session.Session, userID int32) (bool, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if sess == nil || userID <= 0 {
		return false, ErrInvalidInput
	}
	impl := ss.store.Impl()
	row, err := impl.FinalizeUserRegistrationSession(ctx, sess.Data(), userID)
	if err != nil {
		return false, err
	}
	if row == nil {
		sess.Data().MarkStale()
		impl.DeleteCachedUserSession(ctx, sess.ID())
		return false, nil
	}
	sess.Data().SetAuthority(session.StateAuthenticated, row.Version, row.ExpiresAt.Time, ss.now())
	if err := impl.CacheUserSession(ctx, sess.Data()); err != nil {
		slog.WarnContext(ctx, "Failed to cache finalized registration session", common.ErrAttr(err))
	}
	if err := ss.enqueuePersistSessionDelayed(ctx, sess.ID()); err != nil {
		slog.WarnContext(ctx, "Failed to queue finalized registration payload", common.ErrAttr(err))
	}
	return true, nil
}

func (ss *SessionStore) create(ctx context.Context, sess *session.Session, state session.State) error {
	if sess == nil {
		return ErrInvalidInput
	}
	impl := ss.store.Impl()
	cached, err := impl.GetCachedUserSession(ctx, sess.ID())
	if err != nil || cached != sess.Data() {
		return session.ErrSessionMissing
	}
	var userID *int32
	if value, ok := sess.Get(ctx, session.KeyUserID).(int32); ok {
		userID = &value
	}
	if err := impl.CreateUserSessionWithState(ctx, sess.Data(), ss.persistKey, sessionCacheTTL, state, userID); err != nil {
		return err
	}
	sess.Data().MarkValidated(ss.now())
	return nil
}

// Read trusts cached authority within its lease and coalesces revalidation by SID; skipCache forces PostgreSQL.
func (ss *SessionStore) Read(ctx context.Context, sid string, skipCache bool) (*session.Session, error) {
	if !skipCache {
		if sess, done, err := ss.readCached(ctx, sid); done {
			return sess, err
		}

		value, err, _ := ss.validations.Do(sid, func() (any, error) {
			ss.persistMu.Lock()
			defer ss.persistMu.Unlock()
			return ss.read(ctx, sid, false)
		})
		if err != nil {
			return nil, err
		}
		return value.(*session.Session), nil
	}

	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	return ss.read(ctx, sid, skipCache)
}

func (ss *SessionStore) readCached(ctx context.Context, sid string) (*session.Session, bool, error) {
	sd, err := ss.store.Impl().GetCachedUserSession(ctx, sid)
	if err != nil {
		if isSessionCacheMiss(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	if sd.Has(session.KeyTombstone) || sd.IsStale() {
		return nil, true, session.ErrSessionMissing
	}
	if sd.NeedsValidation(ss.now(), sessionValidationLease) {
		return nil, false, nil
	}
	return session.NewSession(sd, ss), true, nil
}

func (ss *SessionStore) read(ctx context.Context, sid string, skipCache bool) (*session.Session, error) {
	impl := ss.store.Impl()
	if !skipCache {
		sd, err := impl.GetCachedUserSession(ctx, sid)
		if err == nil {
			if sd.Has(session.KeyTombstone) || sd.IsStale() {
				return nil, session.ErrSessionMissing
			}
			if !sd.NeedsValidation(ss.now(), sessionValidationLease) {
				return session.NewSession(sd, ss), nil
			}
		} else if !isSessionCacheMiss(err) {
			return nil, err
		}
	}

	sd, err := impl.RetrieveUserSession(ctx, sid, true /*skip cache*/)
	if err != nil {
		if isSessionCacheMiss(err) {
			return nil, session.ErrSessionMissing
		}

		return nil, err
	}

	state, _, _ := sd.Authority()
	if sd.Has(session.KeyTombstone) || sd.IsStale() || state == session.StateRevoked {
		impl.DeleteCachedUserSession(ctx, sid)
		return nil, session.ErrSessionMissing
	}
	sd.MarkValidated(ss.now())
	if cached, cacheErr := impl.GetCachedUserSession(ctx, sid); cacheErr == nil {
		cachedState, _, _ := cached.Authority()
		if cachedState == session.StateAuthenticated && state != session.StateAuthenticated {
			cached.MarkStale()
			impl.DeleteCachedUserSession(ctx, sid)
			return nil, session.ErrSessionMissing
		}
		cached.Replace(sd)
		sd = cached
	} else if isSessionCacheMiss(cacheErr) {
		if err := impl.CacheUserSession(ctx, sd); err != nil {
			return nil, err
		}
	} else {
		return nil, cacheErr
	}

	return session.NewSession(sd, ss), nil
}

func isSessionCacheMiss(err error) bool {
	return err == ErrNegativeCacheHit || err == ErrCacheMiss || err == otter.ErrNotFound
}

// Recover forces an authoritative read and fully replaces the local session data.
func (ss *SessionStore) Recover(ctx context.Context, sess *session.Session) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	dbSession, err := ss.read(ctx, sess.ID(), true)
	if err != nil {
		return err
	}
	sess.Refresh(dbSession)
	return nil
}

// Update queues a best-effort, version-guarded payload write without changing authority.
func (ss *SessionStore) Update(ctx context.Context, sd *session.Session) error {
	if sd == nil {
		return nil
	}
	if sd.Data().RegistrationFinalizing() {
		return nil
	}

	return ss.enqueuePersistSessionDelayed(ctx, sd.ID())
}

func (ss *SessionStore) enqueuePersistSession(ctx context.Context, persistChan chan<- string, sid string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(sessionBackpressureTimeout)
	defer timer.Stop()

	select {
	case persistChan <- sid:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		ss.metrics.ObserveEventDropped(common.SessionEventType)
		return common.ErrBackpressure
	}
}

func (ss *SessionStore) enqueuePersistSessionDelayed(ctx context.Context, sid string) error {
	err := ss.enqueuePersistSession(ctx, ss.persistDelayChan, sid)
	if err == common.ErrBackpressure {
		return nil
	}
	return err
}

// Renew creates the successor before tombstoning and removing the previous SID.
func (ss *SessionStore) Renew(ctx context.Context, oldSID string, sess *session.Session) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()

	impl := ss.store.Impl()
	oldData, _ := impl.GetCachedUserSession(ctx, oldSID)
	if err := ss.Init(ctx, sess); err != nil {
		return err
	}

	persistent := sess.Data().Has(ss.persistKey)
	if persistent {
		if err := ss.create(ctx, sess, session.StateAuthenticated); err != nil {
			impl.DeleteCachedUserSession(ctx, sess.ID())
			slog.ErrorContext(ctx, "Failed to create renewed session", common.SessionIDAttr(sess.ID()), common.ErrAttr(err))
			return err
		}
	}

	tombstone := session.NewTombstoneSessionData(oldSID)
	oldPersisted := false
	if oldData != nil {
		tombstone.CopyPersistence(oldData)
		_, oldPersisted = oldData.Persistence()
	}
	oldSession := session.NewSession(tombstone, ss)
	if err := ss.Init(ctx, oldSession); err != nil {
		ss.rollbackRenew(ctx, oldData, sess.ID(), persistent)
		return err
	}

	if oldPersisted {
		ss.pendingDeletes[oldSID] = struct{}{}
		queueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionBackpressureTimeout)
		err := ss.enqueuePersistSession(queueCtx, ss.persistDelayChan, oldSID)
		cancel()
		if err != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), sessionCleanupTimeout)
			deleteErr := impl.DeleteUserSessions(cleanupCtx, []string{oldSID})
			cleanupCancel()
			if deleteErr == nil {
				delete(ss.pendingDeletes, oldSID)
				return nil
			}
			ss.rollbackRenew(ctx, oldData, sess.ID(), persistent)
			slog.ErrorContext(ctx, "Failed to persist old session tombstone", common.SessionIDAttr(oldSID), common.ErrAttr(deleteErr))
			return deleteErr
		}
	}

	return nil
}

// RenewExpiration queues one heartbeat; callers refresh the cookie only when it returns true.
func (ss *SessionStore) RenewExpiration(ctx context.Context, sess *session.Session) bool {
	if sess == nil || !sess.Data().ClaimExpirationRenewal(ss.now(), sessionExpirationRenewalThreshold) {
		return false
	}
	if err := ss.enqueuePersistSession(ctx, ss.expirationChan, sess.ID()); err != nil {
		sess.Data().RollbackExpirationRenewal()
		return false
	}
	return true
}

func (ss *SessionStore) heartbeatSessions(ctx context.Context, batch map[string]uint) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	if len(batch) == 0 {
		return nil
	}

	sids := make([]string, 0, len(batch))
	for sid := range batch {
		sids = append(sids, sid)
	}
	sort.Strings(sids)
	impl := ss.store.Impl()
	updated, err := impl.ExtendAuthenticatedSessionExpirations(ctx, sids, sessionCacheTTL)
	if err != nil {
		return err
	}

	expirations := make(map[string]time.Time, len(updated))
	for _, row := range updated {
		if row.ExpiresAt.Valid {
			expirations[row.SessionID] = row.ExpiresAt.Time
		}
	}
	for _, sid := range sids {
		sd, err := impl.GetCachedUserSession(ctx, sid)
		if err != nil {
			continue
		}
		if expiresAt, ok := expirations[sid]; ok {
			sd.CompleteExpirationRenewal(expiresAt)
			continue
		}
		sd.MarkStale()
		impl.DeleteCachedUserSession(ctx, sid)
	}
	return nil
}

func (ss *SessionStore) rollbackRenew(ctx context.Context, oldData *session.SessionData, newSID string, persistent bool) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionCleanupTimeout)
	defer cancel()
	impl := ss.store.Impl()
	if persistent {
		if err := impl.DeleteUserSession(rollbackCtx, newSID); err != nil {
			slog.ErrorContext(rollbackCtx, "Failed to delete successor after renewal rollback", common.SessionIDAttr(newSID), common.ErrAttr(err))
			ss.pendingDeletes[newSID] = struct{}{}
			if queueErr := ss.enqueuePersistSession(rollbackCtx, ss.persistDelayChan, newSID); queueErr != nil {
				slog.ErrorContext(rollbackCtx, "Failed to queue successor cleanup after renewal rollback", common.SessionIDAttr(newSID), common.ErrAttr(queueErr))
			}
		}
	} else {
		impl.DeleteCachedUserSession(rollbackCtx, newSID)
	}
	if oldData != nil {
		delete(ss.pendingDeletes, oldData.ID())
		if err := impl.CacheUserSession(rollbackCtx, oldData); err != nil {
			slog.ErrorContext(rollbackCtx, "Failed to restore old session after renewal rollback", common.SessionIDAttr(oldData.ID()), common.ErrAttr(err))
		}
	}
}

func (ss *SessionStore) TTL() time.Duration {
	return sessionCacheTTL
}

// Destroy synchronously revokes one persistent SID, then discards its local copy.
func (ss *SessionStore) Destroy(ctx context.Context, sid string) (session.SessionRevocationResult, error) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	return ss.store.Impl().RevokeUserSession(ctx, sid)
}

func (ss *SessionStore) persistSessions(ctx context.Context, batch map[string]uint) error {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()

	toStore := make(map[string]uint, len(batch))
	toDeleteSet := make(map[string]struct{})
	for sid := range batch {
		if _, pending := ss.pendingDeletes[sid]; pending {
			toDeleteSet[sid] = struct{}{}
		}
	}

	impl := ss.store.Impl()
	cached, err := impl.GetCachedUserSessions(ctx, batch)
	if err == nil {
		for _, sd := range cached {
			sid := sd.ID()
			if sd.Has(session.KeyTombstone) {
				if _, persisted := sd.Persistence(); persisted {
					toDeleteSet[sid] = struct{}{}
					ss.pendingDeletes[sid] = struct{}{}
				}
				continue
			}
			toStore[sid] = batch[sid]
		}

	} else if len(toDeleteSet) != len(batch) {
		slog.ErrorContext(ctx, "Failed to read cached sessions for persistence", common.ErrAttr(err))
		return err
	}
	if err := impl.StoreUserSessions(ctx, toStore, ss.persistKey); err != nil {
		return err
	}
	toDelete := make([]string, 0, len(toDeleteSet))
	for sid := range toDeleteSet {
		toDelete = append(toDelete, sid)
	}
	if err := impl.DeleteUserSessions(ctx, toDelete); err != nil {
		return err
	}
	for _, sid := range toDelete {
		delete(ss.pendingDeletes, sid)
	}

	return nil
}

// RollbackRenew removes a tombstone when the caller keeps the previous SID after failed renewal.
func (ss *SessionStore) RollbackRenew(ctx context.Context, oldSID string) {
	ss.persistMu.Lock()
	defer ss.persistMu.Unlock()
	impl := ss.store.Impl()
	if sd, err := impl.GetCachedUserSession(ctx, oldSID); err == nil && sd.Has(session.KeyTombstone) {
		delete(ss.pendingDeletes, oldSID)
		impl.DeleteCachedUserSession(ctx, oldSID)
	}
}
