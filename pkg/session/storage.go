package session

import (
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type State string

const (
	StatePending       State = "pending"
	StateAuthenticated State = "authenticated"
	StateRevoked       State = "revoked"
)

type ChallengeKind string

const (
	ChallengeKindSignIn       ChallengeKind = "sign_in"
	ChallengeKindRegistration ChallengeKind = "registration"
	ChallengeKindEmailChange  ChallengeKind = "email_change"
)

type StoredSession struct {
	SessionID      string
	State          State
	Version        int32
	UserID         int32
	Payload        []byte
	ExpiresAt      time.Time
	ChallengeKind  ChallengeKind
	ChallengeEmail string
}

type PayloadUpdate struct {
	SessionID       string
	ExpectedVersion int32
	Payload         []byte
}

type PayloadUpdateResult struct {
	SessionID string
	Version   int32
}

type ExpirationRenewalResult struct {
	SessionID string
	Version   int32
	ExpiresAt time.Time
}

type RevocationResult struct {
	SessionID   string
	SessionHash common.SessionHash
	State       State
	Version     int32
	UserID      int32
}
