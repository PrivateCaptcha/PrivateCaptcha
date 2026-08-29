package session

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	ErrSessionMissing = errors.New("session is missing")
)

func init() {
	// for two factor timestamp
	gob.Register(time.Time{})
}

type SessionKey int

const (
	KeyLoginStep SessionKey = iota
	KeyUserID
	KeyUserEmail
	KeyTwoFactorCode
	KeyUserName
	KeyPersistent
	KeyNotificationID
	KeyReturnURL
	KeyTwoFactorCodeTimestamp
	KeyOrgInviteID
	KeyFirstSession
	KeyAdhocNotification
	KeyVerifyRegistration
	KeyTombstone
	KeyLoginAttempts
	// Add new fields _above_
	SESSION_KEYS_COUNT
)

func (key SessionKey) String() string {
	switch key {
	case KeyLoginStep:
		return "LoginStep"
	case KeyUserID:
		return "UserID"
	case KeyUserEmail:
		return "UserEmail"
	case KeyTwoFactorCode:
		return "TwoFactorCode"
	case KeyTwoFactorCodeTimestamp:
		return "TwoFactorCodeTimestamp"
	case KeyUserName:
		return "UserName"
	case KeyPersistent:
		return "Persistent"
	case KeyNotificationID:
		return "NotificationID"
	case KeyReturnURL:
		return "ReturnURL"
	case KeyOrgInviteID:
		return "OrgInviteID"
	case KeyFirstSession:
		return "FirstSession"
	case KeyVerifyRegistration:
		return "VerifyRegistration"
	case KeyTombstone:
		return "Tombstone"
	default:
		return "SessionKey"
	}
}

type SessionValue = interface{}

type State string

const (
	StatePending                State = "pending"
	StateRegistrationProcessing State = "registration_processing"
	StateAuthenticated          State = "authenticated"
	StateRevoked                State = "revoked"
)

type SignInChallengeResult struct {
	Consumed          bool
	AttemptsExhausted bool
}

type SignInChallengeReissue struct {
	EncodedCode string
	Email       string
}

type RegistrationChallengeResult struct {
	Consumed          bool
	Verified          bool
	AttemptsExhausted bool
	Email             string
}

type RegistrationChallengeReissue struct {
	EncodedCode string
	Email       string
}

type EmailChangeChallengeIssue struct {
	EncodedCode string
	Email       string
}

type EmailChangeChallengeResult struct {
	Consumed          bool
	AttemptsExhausted bool
	Email             string
	AuditEvent        *common.AuditLogEvent
}

type SessionRevocationResult struct {
	UserID       int32
	Transitioned bool
}

type SessionData struct {
	sid    string
	values map[SessionKey]SessionValue
	// state is local lifecycle state; it can lead PostgreSQL during registration finalization.
	state State
	// version is the PostgreSQL generation used for payload CAS writes.
	version int32
	// expiresAt is the latest PostgreSQL-confirmed expiration.
	expiresAt time.Time
	// validatedAt starts the trust lease after database validation or local registration.
	validatedAt time.Time
	lock        sync.Mutex
	// persisted marks a session as database-backed; it does not prove the row still exists.
	persisted bool
	// dirty marks continuation data accepted after the current database generation.
	dirty bool
	// stale marks the copy unusable after local invalidation or a rejected database operation.
	// Lease expiry alone does not make a session stale.
	stale bool
	// expirationRenewalPending prevents duplicate heartbeats; it does not extend expiration.
	expirationRenewalPending bool
	// registrationFinalizing marks local authentication awaiting guarded database promotion.
	registrationFinalizing bool
}

// NewSessionData creates a local-only session with no PostgreSQL authority.
func NewSessionData(sid string) *SessionData {
	return &SessionData{
		sid:    sid,
		values: make(map[SessionKey]SessionValue),
	}
}

// NewTombstoneSessionData blocks old SID reuse and supports deleting its persistent row.
func NewTombstoneSessionData(sid string) *SessionData {
	sd := NewSessionData(sid)
	sd.values[KeyTombstone] = true
	return sd
}

func (sd *SessionData) Size() int {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return len(sd.values)
}

func (sd *SessionData) MarshalBinary() ([]byte, error) {
	data, _, _, err := sd.PersistenceSnapshot()
	return data, err
}

// PersistenceSnapshot atomically captures the payload and CAS version for persistence.
func (sd *SessionData) PersistenceSnapshot() ([]byte, int32, bool, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	sd.lock.Lock()
	defer sd.lock.Unlock()

	if err := encoder.Encode(sd.values); err != nil {
		return nil, 0, false, err
	}

	return buf.Bytes(), sd.version, sd.persisted, nil
}

func (sd *SessionData) UnmarshalBinary(data []byte) error {
	values := make(map[SessionKey]SessionValue)

	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	if err := decoder.Decode(&values); err != nil {
		return err
	}

	sd.lock.Lock()
	sd.values = values
	sd.lock.Unlock()

	return nil
}

// GobEncode serializes only the SID and continuation data, never authority metadata.
func (sd *SessionData) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	sd.lock.Lock()
	defer sd.lock.Unlock()

	if err := encoder.Encode(sd.sid); err != nil {
		return nil, err
	}
	if err := encoder.Encode(sd.values); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (sd *SessionData) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)

	var sid string
	if err := decoder.Decode(&sid); err != nil {
		return err
	}
	var values map[SessionKey]SessionValue
	if err := decoder.Decode(&values); err != nil {
		return err
	}

	sd.lock.Lock()
	sd.sid = sid
	sd.values = values
	sd.lock.Unlock()

	return nil
}

// Merge copies continuation data without copying the source SID or authority metadata.
func (sd *SessionData) Merge(from *SessionData, overwrite bool) {
	if sd == from {
		return
	}

	// Acquire locks in consistent order to prevent deadlock
	first, second := sd, from
	if sd.sid > from.sid {
		first, second = from, sd
	}

	first.lock.Lock()
	defer first.lock.Unlock()

	second.lock.Lock()
	defer second.lock.Unlock()

	for key, value := range from.values {
		if _, ok := sd.values[key]; !ok || overwrite {
			sd.values[key] = value
		}
	}
}

// Replace fully refreshes a cached copy, removing fields absent from the database read.
func (sd *SessionData) Replace(from *SessionData) {
	if sd == from {
		return
	}

	from.lock.Lock()
	values := make(map[SessionKey]SessionValue, len(from.values))
	for key, value := range from.values {
		values[key] = value
	}
	version := from.version
	persisted := from.persisted
	dirty := from.dirty
	stale := from.stale
	state := from.state
	expiresAt := from.expiresAt
	validatedAt := from.validatedAt
	expirationRenewalPending := from.expirationRenewalPending
	registrationFinalizing := from.registrationFinalizing
	from.lock.Unlock()

	sd.lock.Lock()
	sd.values = values
	sd.version = version
	sd.persisted = persisted
	sd.dirty = dirty
	sd.stale = stale
	sd.state = state
	sd.expiresAt = expiresAt
	sd.validatedAt = validatedAt
	sd.expirationRenewalPending = expirationRenewalPending
	sd.registrationFinalizing = registrationFinalizing
	sd.lock.Unlock()
}

func (sd *SessionData) Persistence() (int32, bool) {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return sd.version, sd.persisted
}

// SetPersistence marks a copy database-backed at version, primarily for tombstones.
func (sd *SessionData) SetPersistence(version int32) {
	sd.lock.Lock()
	sd.version = version
	sd.persisted = true
	sd.stale = false
	sd.lock.Unlock()
}

// SetAuthority installs a PostgreSQL authority baseline and clears transient local state.
func (sd *SessionData) SetAuthority(state State, version int32, expiresAt, validatedAt time.Time) {
	sd.lock.Lock()
	sd.state = state
	sd.version = version
	sd.persisted = true
	sd.dirty = false
	sd.stale = false
	sd.expiresAt = expiresAt
	sd.validatedAt = validatedAt
	sd.expirationRenewalPending = false
	sd.registrationFinalizing = false
	sd.lock.Unlock()
}

// SetAuthoritativeUserID replaces any payload value with the PostgreSQL user ID.
func (sd *SessionData) SetAuthoritativeUserID(userID int32) {
	sd.set(KeyUserID, userID)
}

func (sd *SessionData) UserID() (int32, bool) {
	value, ok := sd.get(KeyUserID)
	userID, valid := value.(int32)
	return userID, ok && valid && userID > 0
}

func (sd *SessionData) Authority() (State, time.Time, time.Time) {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return sd.state, sd.expiresAt, sd.validatedAt
}

// MarkValidated starts a lease after checking the session against PostgreSQL authority.
func (sd *SessionData) MarkValidated(validatedAt time.Time) {
	sd.lock.Lock()
	sd.validatedAt = validatedAt
	sd.lock.Unlock()
}

// MarkStale makes the copy unusable and cancels any pending expiration renewal.
func (sd *SessionData) MarkStale() {
	sd.lock.Lock()
	sd.stale = true
	sd.dirty = false
	sd.expirationRenewalPending = false
	sd.lock.Unlock()
}

// MarkRegistrationAuthenticatedLocally authenticates the registration winner
// while guarded database finalization is pending and blocks normal persistence.
func (sd *SessionData) MarkRegistrationAuthenticatedLocally(validatedAt time.Time) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.persisted || sd.stale || sd.state != StateRegistrationProcessing {
		return false
	}
	sd.state = StateAuthenticated
	sd.validatedAt = validatedAt
	sd.registrationFinalizing = true
	return true
}

func (sd *SessionData) RegistrationFinalizing() bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return sd.registrationFinalizing
}

// NeedsValidation requires a database refresh after the authentication lease or
// expiration and throughout registration processing.
func (sd *SessionData) NeedsValidation(now time.Time, lease time.Duration) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.persisted {
		return false
	}
	if sd.state == StateRegistrationProcessing {
		return true
	}
	if sd.state != StateAuthenticated {
		return false
	}
	if sd.expiresAt.IsZero() || !now.Before(sd.expiresAt) {
		return true
	}
	return sd.validatedAt.IsZero() || !now.Before(sd.validatedAt.Add(lease))
}

// ClaimExpirationRenewal reserves one heartbeat near expiry. The store queues a
// successful claim, and only CompleteExpirationRenewal extends the local deadline.
func (sd *SessionData) ClaimExpirationRenewal(now time.Time, threshold time.Duration) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.persisted || sd.stale || sd.state != StateAuthenticated || sd.expirationRenewalPending {
		return false
	}
	if threshold <= 0 || sd.expiresAt.IsZero() || !now.Before(sd.expiresAt) {
		return false
	}
	if sd.expiresAt.Sub(now) >= threshold {
		return false
	}
	sd.expirationRenewalPending = true
	return true
}

// CompleteExpirationRenewal applies a confirmed deadline and releases the heartbeat claim.
func (sd *SessionData) CompleteExpirationRenewal(expiresAt time.Time) {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.persisted || sd.stale || sd.state != StateAuthenticated {
		return
	}
	sd.expiresAt = expiresAt
	sd.expirationRenewalPending = false
}

// RollbackExpirationRenewal clears an unqueued claim so a later request can retry.
func (sd *SessionData) RollbackExpirationRenewal() {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.expirationRenewalPending {
		return
	}
	sd.expirationRenewalPending = false
}

// AdvancePersistence moves the local CAS baseline after a successful payload write.
func (sd *SessionData) AdvancePersistence(expectedVersion, version int32) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.persisted || sd.version != expectedVersion {
		return false
	}
	sd.version = version
	sd.stale = false
	sd.dirty = false
	return true
}

// InvalidatePersistence marks the current snapshot stale after a failed payload CAS.
// The version guard prevents an old failure from invalidating newer authority.
func (sd *SessionData) InvalidatePersistence(expectedVersion int32) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.persisted || sd.version != expectedVersion {
		return false
	}
	sd.stale = true
	sd.dirty = false
	return true
}

func (sd *SessionData) MarkDirty() {
	sd.lock.Lock()
	sd.dirty = true
	sd.lock.Unlock()
}

func (sd *SessionData) IsDirty() bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return sd.dirty
}

// AdoptAuthorityIfDirty refreshes metadata without replacing locally accepted
// continuation data when PostgreSQL is still at the same generation.
func (sd *SessionData) AdoptAuthorityIfDirty(from *SessionData) bool {
	if sd == from {
		return true
	}
	from.lock.Lock()
	state := from.state
	version := from.version
	persisted := from.persisted
	stale := from.stale
	expiresAt := from.expiresAt
	validatedAt := from.validatedAt
	from.lock.Unlock()

	sd.lock.Lock()
	defer sd.lock.Unlock()
	if !sd.dirty || !sd.persisted || sd.version != version || !persisted || stale {
		return false
	}
	sd.state = state
	sd.expiresAt = expiresAt
	sd.validatedAt = validatedAt
	return true
}

func (sd *SessionData) IsStale() bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()
	return sd.stale
}

// CopyPersistence transfers the persistence marker and version to a tombstone.
func (sd *SessionData) CopyPersistence(from *SessionData) {
	version, persisted := from.Persistence()
	if !persisted {
		return
	}
	sd.SetPersistence(version)
}

func (sd *SessionData) ID() string {
	return sd.sid
}

func (sd *SessionData) Has(key SessionKey) bool {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	_, ok := sd.values[key]
	return ok
}

func (sd *SessionData) set(key SessionKey, value SessionValue) {
	sd.lock.Lock()
	sd.values[key] = value
	sd.lock.Unlock()
}

func (sd *SessionData) get(key SessionKey) (any, bool) {
	sd.lock.Lock()
	defer sd.lock.Unlock()

	v, ok := sd.values[key]
	return v, ok
}

func (sd *SessionData) delete(key SessionKey) {
	sd.lock.Lock()
	delete(sd.values, key)
	sd.lock.Unlock()
}

type Session struct {
	data  *SessionData
	store Store
}

func NewSession(data *SessionData, store Store) *Session {
	return &Session{
		data:  data,
		store: store,
	}
}

func (s *Session) Merge(from *Session) {
	s.data.Merge(from.data, false /*overwrite*/)
}

func (s *Session) Refresh(from *Session) {
	s.data.Replace(from.data)
}

func (s *Session) Data() *SessionData {
	return s.data
}

func (s *Session) Set(ctx context.Context, key SessionKey, value SessionValue) error {
	return s.store.Update(ctx, s, func() { s.data.set(key, value) })
}

func (s *Session) Persist(ctx context.Context) error {
	return s.persist(func() error { return s.store.Create(ctx, s) })
}

func (s *Session) PersistSignInChallenge(ctx context.Context, encodedCode, email string, challengeTTL time.Duration) error {
	return s.persist(func() error {
		return s.store.CreateSignInChallenge(ctx, s, encodedCode, email, challengeTTL)
	})
}

func (s *Session) PersistRegistrationChallenge(ctx context.Context, encodedCode, email string, challengeTTL time.Duration) error {
	return s.persist(func() error {
		return s.store.CreateRegistrationChallenge(ctx, s, encodedCode, email, challengeTTL)
	})
}

func (s *Session) persist(create func() error) error {
	s.data.set(KeyPersistent, true)
	if err := create(); err != nil {
		s.data.delete(KeyPersistent)
		return err
	}
	return nil
}

func (s *Session) ReissueSignInChallenge(ctx context.Context, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (SignInChallengeReissue, error) {
	return s.store.ReissueSignInChallenge(ctx, s, encodedCode, fallbackEncodedCode, challengeTTL)
}

func (s *Session) ReissueRegistrationChallenge(ctx context.Context, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (RegistrationChallengeReissue, error) {
	return s.store.ReissueRegistrationChallenge(ctx, s, encodedCode, fallbackEncodedCode, challengeTTL)
}

func (s *Session) FinalizeRegistration(ctx context.Context, userID int32) (bool, error) {
	return s.store.FinalizeRegistration(ctx, s, userID)
}

func (s *Session) IssueEmailChangeChallenge(ctx context.Context, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (EmailChangeChallengeIssue, error) {
	return s.store.IssueEmailChangeChallenge(ctx, s, encodedCode, fallbackEncodedCode, challengeTTL)
}

func (s *Session) ConsumeEmailChangeChallenge(ctx context.Context, newEmail, encodedCode string, maxFailedAttempts int32) (EmailChangeChallengeResult, error) {
	return s.store.ConsumeEmailChangeChallenge(ctx, s, newEmail, encodedCode, maxFailedAttempts)
}

func (s *Session) ID() string {
	return s.data.ID()
}

func (s *Session) Get(ctx context.Context, key SessionKey) SessionValue {
	v, ok := s.data.get(key)
	if !ok {
		slog.Log(ctx, common.LevelTrace, "Access to missing key in session", common.SessionIDAttr(s.data.ID()), "key", key.String())
	}

	return v
}

func (s *Session) Delete(ctx context.Context, key SessionKey) error {
	return s.store.Update(ctx, s, func() { s.data.delete(key) })
}

type Store interface {
	Start(ctx context.Context, interval time.Duration)
	Init(ctx context.Context, session *Session) error
	Create(ctx context.Context, session *Session) error
	CreateSignInChallenge(ctx context.Context, session *Session, encodedCode, email string, challengeTTL time.Duration) error
	CreateRegistrationChallenge(ctx context.Context, session *Session, encodedCode, email string, challengeTTL time.Duration) error
	ConsumeSignInChallenge(ctx context.Context, current, successor *Session, prepareSuccessor func(), encodedCode string, maxFailedAttempts int32) (SignInChallengeResult, error)
	ConsumeRegistrationChallenge(ctx context.Context, current, successor *Session, prepareSuccessor func(), encodedCode string, maxFailedAttempts int32, allowConsumption bool) (RegistrationChallengeResult, error)
	ReissueSignInChallenge(ctx context.Context, session *Session, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (SignInChallengeReissue, error)
	ReissueRegistrationChallenge(ctx context.Context, session *Session, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (RegistrationChallengeReissue, error)
	FinalizeRegistration(ctx context.Context, session *Session, userID int32) (bool, error)
	IssueEmailChangeChallenge(ctx context.Context, session *Session, encodedCode, fallbackEncodedCode string, challengeTTL time.Duration) (EmailChangeChallengeIssue, error)
	ConsumeEmailChangeChallenge(ctx context.Context, session *Session, newEmail, encodedCode string, maxFailedAttempts int32) (EmailChangeChallengeResult, error)
	Read(ctx context.Context, sid string, skipCache bool) (*Session, error)
	Recover(ctx context.Context, session *Session) error
	Update(ctx context.Context, session *Session, update func()) error
	Renew(ctx context.Context, current, successor *Session, prepareSuccessor func()) error
	RenewExpiration(ctx context.Context, session *Session) bool
	Destroy(ctx context.Context, sid string) (SessionRevocationResult, error)
}
