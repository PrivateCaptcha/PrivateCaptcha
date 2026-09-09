package session

import (
	"context"
	"time"
)

// StubSessionStore implements Store with no-ops.
type StubSessionStore struct{}

func (s *StubSessionStore) Start(_ context.Context, _ time.Duration) {}
func (s *StubSessionStore) EnqueueExpirationRenewal(_ context.Context, _ string) {
}
func (s *StubSessionStore) UpdatePayload(_ context.Context, _ string) {}
func (s *StubSessionStore) StartAnonymousSession(sid string) *Session {
	return NewAnonymousSession(sid, s)
}
func (s *StubSessionStore) Resolve(context.Context, string) (*Session, error) {
	return nil, ErrSessionMissing
}
func (s *StubSessionStore) IssueSignInChallenge(context.Context, SignInChallengeIssue) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) IssueRegistrationChallenge(context.Context, RegistrationChallengeIssue) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) SetVerifyRegistration(context.Context, string, bool) error {
	return nil
}
func (s *StubSessionStore) ResendPendingChallenge(context.Context, PendingChallengeResend) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) ConsumeSignInChallenge(context.Context, SignInChallengeConsume) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) ConsumeRegistrationChallenge(context.Context, RegistrationChallengeConsume) (*RegistrationConsumeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) CreateRegistrationSuccessor(context.Context, RegistrationSuccessorCreate) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) IssueEmailChangeChallenge(context.Context, EmailChangeChallengeIssue) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) ConsumeEmailChangeChallenge(context.Context, EmailChangeChallengeConsume) (*ChallengeResult, error) {
	return nil, nil
}
func (s *StubSessionStore) RevokeSession(context.Context, string) (*RevocationResult, error) {
	return nil, nil
}
func (s *StubSessionStore) RevokeUserSessions(context.Context, int32) error { return nil }

type PayloadStoreStub struct {
	Update func(context.Context, string)
}

func (s PayloadStoreStub) UpdatePayload(ctx context.Context, sid string) {
	if s.Update != nil {
		s.Update(ctx, sid)
	}
}
