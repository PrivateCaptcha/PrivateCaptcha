package session

import (
	"errors"
	"time"
)

var (
	ErrInvalidPayload          = errors.New("invalid session Payload")
	ErrInvalidTransitionResult = errors.New("invalid session transition result")
)

type TransitionOutcome string

const (
	TransitionSucceeded            TransitionOutcome = "succeeded"
	TransitionInvalidCode          TransitionOutcome = "invalid_code"
	TransitionExpired              TransitionOutcome = "expired"
	TransitionAttemptsExhausted    TransitionOutcome = "attempts_exhausted"
	TransitionVerificationRequired TransitionOutcome = "verification_required"
	TransitionStale                TransitionOutcome = "stale"
	TransitionMissing              TransitionOutcome = "missing"
)

type ChallengeIssueResult struct {
	Outcome TransitionOutcome
	Session *StoredSession
}

type SignInChallengeConsumeResult struct {
	Outcome        TransitionOutcome
	Session        *StoredSession
	FailedAttempts int32
	Successor      *StoredSession
}

type RegistrationChallengeConsumeResult struct {
	Outcome        TransitionOutcome
	Session        *StoredSession
	FailedAttempts int32
	Email          string
	Name           string
	InviteID       int32
}

type EmailChangeChallengeConsumeResult struct {
	Outcome        TransitionOutcome
	Session        *StoredSession
	FailedAttempts int32
}

type RegistrationSuccessorResult struct {
	Outcome TransitionOutcome
	Session *StoredSession
}

type ChallengeResult struct {
	Outcome        TransitionOutcome
	Session        *Session
	Authority      Authority
	FailedAttempts int32
}

type RegistrationConsumeResult struct {
	Outcome        TransitionOutcome
	FailedAttempts int32
	Email          string
	Name           string
	InviteID       int32
}

type SignInChallengeIssue struct {
	SessionID     string
	UserID        int32
	ChallengeCode string
	Payload       []byte
	SessionTTL    time.Duration
	ChallengeTTL  time.Duration
	MaxAttempts   int32
}

type RegistrationChallengeIssue struct {
	SessionID      string
	ChallengeEmail string
	ChallengeCode  string
	Payload        []byte
	InviteID       int32
	SessionTTL     time.Duration
	ChallengeTTL   time.Duration
	MaxAttempts    int32
}

type PendingChallengeResend struct {
	SessionID     string
	ChallengeCode string
	ChallengeTTL  time.Duration
	MaxAttempts   int32
}

type SignInChallengeConsume struct {
	SessionID          string
	SuccessorSessionID string
	ChallengeCode      string
	SuccessorPayload   []byte
	SuccessorTTL       time.Duration
	MaxAttempts        int32
}

type RegistrationChallengeConsume struct {
	SessionID     string
	ChallengeCode string
	MaxAttempts   int32
}

type RegistrationSuccessorCreate struct {
	SessionID string
	UserID    int32
	Payload   []byte
	TTL       time.Duration
}

type EmailChangeChallengeIssue struct {
	SessionID     string
	ChallengeCode string
	ChallengeTTL  time.Duration
}

type EmailChangeChallengeConsume struct {
	SessionID     string
	ChallengeCode string
	MaxAttempts   int32
}

func RegistrationNameFromPayload(data []byte) (string, error) {
	payload, err := decodePayload(data)
	if err != nil {
		return "", err
	}
	name, ok := payload[KeyUserName]
	if !ok {
		return "", ErrInvalidPayload
	}
	value, ok := name.(string)
	if !ok || value == "" {
		return "", ErrInvalidPayload
	}
	return value, nil
}
