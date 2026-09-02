package portal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
	"github.com/jackc/pgx/v5"
)

func newSessionStorageTest(t *testing.T) (pgx.Tx, *db.BusinessStoreImpl) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tx, err := store.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
	})

	business := db.NewBusinessWithQuerier(nil, dbgen.New(tx), db.NewStaticCache[db.CacheKey, any](1, &db.CacheMissingValue{}))
	return tx, business.Impl()
}

func TestSessionStorageSchema(t *testing.T) {
	tx, _ := newSessionStorageTest(t)
	ctx := t.Context()

	var userID int32
	if err := tx.QueryRow(ctx,
		"INSERT INTO backend.users (name, email) VALUES ($1, $2) RETURNING id",
		t.Name(), t.Name()+"@privatecaptcha.com",
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	var version, failedAttempts int32
	if err := tx.QueryRow(ctx, `
		INSERT INTO backend.sessions (
			session_id, state, user_id, data, expires_at,
			challenge_kind, challenge_code, challenge_email, challenge_expires_at,
			registration_requires_verification, registration_invite_id
		) VALUES ($1, 'authenticated', $2, 'payload', NOW() + INTERVAL '1 hour',
			'email_change', '123456', $3, NOW() + INTERVAL '15 minutes', TRUE, 42)
		RETURNING version, failed_attempts
	`, t.Name(), userID, t.Name()+"@privatecaptcha.com").Scan(&version, &failedAttempts); err != nil {
		t.Fatal(err)
	}
	if version != 1 || failedAttempts != 0 {
		t.Fatalf("defaults = (%d, %d), want (1, 0)", version, failedAttempts)
	}

	var indexCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'backend'
		  AND indexname IN ('sessions_expires_at_idx', 'sessions_user_id_idx')
	`).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 2 {
		t.Fatalf("session index count = %d, want 2", indexCount)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM backend.users WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	var sessionCount int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM backend.sessions WHERE session_id = $1", t.Name()).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Fatalf("session count after user deletion = %d, want 0", sessionCount)
	}
}

func TestSessionStorageLifecycle(t *testing.T) {
	tx, impl := newSessionStorageTest(t)
	ctx := t.Context()
	email := t.Name() + "@privatecaptcha.com"
	sid := t.Name() + "-authenticated"
	pendingSID := t.Name() + "-pending"
	farFutureSID := t.Name() + "-far-future"
	expiredSID := t.Name() + "-expired"

	var userID int32
	if err := tx.QueryRow(ctx,
		"INSERT INTO backend.users (name, email) VALUES ($1, $2) RETURNING id", t.Name(), email,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, 'old', NOW() + INTERVAL '10 minutes')
	`, sid, userID); err != nil {
		t.Fatal(err)
	}

	record, err := impl.RetrieveLiveSession(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != session.StateAuthenticated || record.UserID != userID || record.Version != 1 || string(record.Payload) != "old" {
		t.Fatalf("unexpected session: %+v", record)
	}
	initialExpiration := record.ExpiresAt

	if _, err := tx.Exec(ctx, "UPDATE backend.users SET enabled = FALSE WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	_, err = impl.RetrieveLiveSession(ctx, sid)
	assertSessionMissing(t, err)
	if _, err := tx.Exec(ctx, "UPDATE backend.users SET enabled = TRUE, deleted_at = NOW() WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	_, err = impl.RetrieveLiveSession(ctx, sid)
	assertSessionMissing(t, err)
	if _, err := tx.Exec(ctx, "UPDATE backend.users SET deleted_at = NULL WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, data, expires_at)
		VALUES ($1, 'authenticated', '', NOW() + INTERVAL '1 hour')
	`, t.Name()+"-userless"); err != nil {
		t.Fatal(err)
	}
	_, err = impl.RetrieveLiveSession(ctx, t.Name()+"-userless")
	assertSessionMissing(t, err)

	updated, err := impl.UpdateSessionPayloads(ctx, []session.PayloadUpdate{
		{SessionID: sid, ExpectedVersion: 1, Payload: []byte("new")},
		{SessionID: t.Name() + "-missing", ExpectedVersion: 1, Payload: []byte("missing")},
	})
	if err != nil || len(updated) != 1 || updated[0].Version != 2 {
		t.Fatalf("Payload update = (%+v, %v), want one result at version 2", updated, err)
	}
	record, err = impl.RetrieveLiveSession(ctx, sid)
	if err != nil || record.Version != 2 || string(record.Payload) != "new" {
		t.Fatalf("persisted Payload = (%+v, %v), want new at version 2", record, err)
	}
	stale, err := impl.UpdateSessionPayloads(ctx, []session.PayloadUpdate{{SessionID: sid, ExpectedVersion: 1, Payload: []byte("stale")}})
	if err != nil || len(stale) != 0 {
		t.Fatalf("stale Payload update = (%+v, %v), want no results", stale, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at, challenge_kind, challenge_code, challenge_email)
		VALUES ($1, 'pending', $2, '', NOW() + INTERVAL '10 minutes', 'sign_in', '123456', $3)
	`, pendingSID, userID, email); err != nil {
		t.Fatal(err)
	}
	var farFutureExpiration time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, '', NOW() + INTERVAL '6 hours')
		RETURNING expires_at
	`, farFutureSID, userID).Scan(&farFutureExpiration); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, '', NOW() - INTERVAL '1 hour')
	`, expiredSID, userID); err != nil {
		t.Fatal(err)
	}
	renewed, err := impl.RenewSessionExpirations(ctx, []string{sid, pendingSID, farFutureSID, expiredSID}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	renewals := make(map[string]session.ExpirationRenewalResult, len(renewed))
	for _, result := range renewed {
		renewals[result.SessionID] = result
	}
	if len(renewals) != 2 || renewals[sid].Version != 2 || !renewals[sid].ExpiresAt.After(initialExpiration) {
		t.Fatalf("renewals = %+v, want live authenticated rows only", renewals)
	}
	if farFuture := renewals[farFutureSID]; farFuture.Version != 1 || !farFuture.ExpiresAt.Equal(farFutureExpiration) {
		t.Fatalf("far-future renewal = %+v, want unchanged expiration %s and version 1", farFuture, farFutureExpiration)
	}

	if _, err := tx.Exec(ctx, "UPDATE backend.sessions SET challenge_code = '123456' WHERE session_id = $1", sid); err != nil {
		t.Fatal(err)
	}
	first, err := impl.RevokeSession(ctx, sid)
	if err != nil || first == nil || first.State != session.StateRevoked || first.Version != 3 || first.UserID != userID {
		t.Fatalf("first revocation = (%+v, %v), want revoked version 3", first, err)
	}
	assertSessionChallengeCodeCleared(t, tx, sid)
	repeated, err := impl.RevokeSession(ctx, sid)
	if err != nil || repeated == nil || repeated.Version != 3 {
		t.Fatalf("repeated revocation = (%+v, %v), want version 3", repeated, err)
	}
	_, err = impl.RetrieveLiveSession(ctx, sid)
	assertSessionMissing(t, err)
	revoked, err := impl.RevokeUserSessions(ctx, userID)
	versions := make(map[string]int32, len(revoked))
	for _, result := range revoked {
		versions[result.SessionID] = result.Version
	}
	if err != nil || len(revoked) != 3 || versions[sid] != 3 || versions[pendingSID] != 2 || versions[farFutureSID] != 2 {
		t.Fatalf("user revocation = (%v, %v), want versions 3, 2, and 2", versions, err)
	}
	assertSessionChallengeCodeCleared(t, tx, pendingSID)
	blocked, err := impl.UpdateSessionPayloads(ctx, []session.PayloadUpdate{{SessionID: sid, ExpectedVersion: 3, Payload: []byte("blocked")}})
	if err != nil || len(blocked) != 0 {
		t.Fatalf("revoked Payload update = (%+v, %v), want no results", blocked, err)
	}

	exactSID := t.Name() + "-exact"
	futureSID := t.Name() + "-future"
	if _, err := tx.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, data, expires_at, challenge_kind, challenge_email)
		VALUES
			($1, 'pending', '', NOW(), 'registration', $3),
			($2, 'pending', '', NOW() + INTERVAL '1 hour', 'registration', $3)
	`, exactSID, futureSID, email); err != nil {
		t.Fatal(err)
	}
	_, err = impl.RetrieveLiveSession(ctx, exactSID)
	assertSessionMissing(t, err)
	if _, err := impl.RetrieveLiveSession(ctx, futureSID); err != nil {
		t.Fatalf("future session read failed: %v", err)
	}
	if err := impl.DeleteExpiredSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var exactCount, futureCount int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE session_id = $1), COUNT(*) FILTER (WHERE session_id = $2)
		FROM backend.sessions
	`, exactSID, futureSID).Scan(&exactCount, &futureCount); err != nil {
		t.Fatal(err)
	}
	if exactCount != 0 || futureCount != 1 {
		t.Fatalf("cleanup counts = (%d, %d), want (0, 1)", exactCount, futureCount)
	}
}

type sessionChallengeQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assertSessionChallengeCodeCleared(t *testing.T, querier sessionChallengeQuerier, sid string) {
	t.Helper()
	var cleared bool
	if err := querier.QueryRow(t.Context(),
		"SELECT challenge_code IS NULL FROM backend.sessions WHERE session_id = $1", sid,
	).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatalf("session %q retained challenge code", sid)
	}
}

func assertSessionMissing(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, db.ErrRecordNotFound) {
		t.Fatalf("error = %v, want %v", err, db.ErrRecordNotFound)
	}
}
