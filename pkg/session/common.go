package session

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

var (
	ErrSessionMissing = errors.New("session is missing")
)

type SessionKey int

const (
	KeyUserEmail         SessionKey = 2
	KeyUserName          SessionKey = 4
	KeyNotificationID    SessionKey = 6
	KeyReturnURL         SessionKey = 7
	KeyOrgInviteID       SessionKey = 9
	KeyFirstSession      SessionKey = 10
	KeyAdhocNotification SessionKey = 11
)

type SessionValue = interface{}

type Session struct {
	sid       string
	hash      common.SessionHash
	authority atomic.Pointer[Authority]
	payload   *Payload
}

func (s *Session) Set(ctx context.Context, key SessionKey, value SessionValue) error {
	return s.payload.Set(ctx, key, value)
}

func (s *Session) ID() string {
	return s.sid
}

func (s *Session) Hash() common.SessionHash {
	return s.hash
}

func (s *Session) Get(ctx context.Context, key SessionKey) SessionValue {
	return s.payload.Get(key)
}

func (s *Session) Delete(ctx context.Context, key SessionKey) error {
	return s.payload.Delete(ctx, key)
}

// Consume logic:
// Sign-in atomically revokes and inserts a successor, registration revokes and returns data for a later account transaction,
// while email change keeps the session authenticated and only clears challenge columns.
type Store interface {
	EnqueueExpirationRenewal(ctx context.Context, sid string)
	StartAnonymousSession(sid string) *Session
	Resolve(ctx context.Context, sid string) (*Session, error)
	IssueSignInChallenge(ctx context.Context, issue SignInChallengeIssue) (*ChallengeResult, error)
	IssueRegistrationChallenge(ctx context.Context, issue RegistrationChallengeIssue) (*ChallengeResult, error)
	SetVerifyRegistration(ctx context.Context, sid string) error
	ResendPendingChallenge(ctx context.Context, resend PendingChallengeResend) (*ChallengeResult, error)
	ConsumeSignInChallenge(ctx context.Context, consume SignInChallengeConsume) (*ChallengeResult, error)
	ConsumeRegistrationChallenge(ctx context.Context, consume RegistrationChallengeConsume) (*RegistrationConsumeResult, error)
	CreateRegistrationSuccessor(ctx context.Context, create RegistrationSuccessorCreate) (*ChallengeResult, error)
	IssueEmailChangeChallenge(ctx context.Context, issue EmailChangeChallengeIssue) (*ChallengeResult, error)
	ConsumeEmailChangeChallenge(ctx context.Context, consume EmailChangeChallengeConsume) (*ChallengeResult, error)
	RevokeSession(ctx context.Context, sid string) (*RevocationResult, error)
	RevokeUserSessions(ctx context.Context, userID int32) error
	Start(ctx context.Context, interval time.Duration)
}
