package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/email"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/session"
)

func TestEmailChangeChallengeGuards(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	for _, stage := range []string{"CurrentAddress", "NewAddress"} {
		for _, tc := range []struct {
			name  string
			query string
			user  bool
		}{
			{
				name:  "ExpiredCode",
				query: "UPDATE backend.sessions SET challenge_expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1",
			},
			{
				name:  "ExpiredSession",
				query: "UPDATE backend.sessions SET expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1",
			},
			{
				name:  "Revoked",
				query: "UPDATE backend.sessions SET state = 'revoked' WHERE session_id = $1",
			},
			{
				name:  "Exhausted",
				query: "UPDATE backend.sessions SET failed_attempts = 5 WHERE session_id = $1",
			},
			{
				name:  "DisabledUser",
				query: "UPDATE backend.users SET enabled = FALSE WHERE id = $1",
				user:  true,
			},
			{
				name:  "DeletedUser",
				query: "UPDATE backend.users SET deleted_at = NOW() WHERE id = $1",
				user:  true,
			},
			{
				name:  "WrongStage",
				query: "UPDATE backend.sessions SET challenge_kind = 'sign_in' WHERE session_id = $1",
			},
			{
				name:  "ChangedRecipient",
				query: "UPDATE backend.sessions SET challenge_email = 'different@example.com' WHERE session_id = $1",
			},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				ctx := t.Context()
				prefix := transitionTestPrefix(t)
				userID, email := transitionTestUser(t, prefix)
				sid := prefix + "-session"
				newEmail := prefix + "-new@example.com"
				kind := session.ChallengeKindEmailChange
				if stage == "NewAddress" {
					kind = session.ChallengeKindEmailChangeNew
					email = newEmail
				}
				if _, err := store.Pool.Exec(ctx, `
					INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at,
					    challenge_kind, challenge_code, challenge_email, challenge_expires_at)
					VALUES ($1, 'authenticated', $2, '', NOW() + INTERVAL '3 hours', $3, 'ABC123', $4, NOW() + INTERVAL '15 minutes')
				`, sid, userID, kind, email); err != nil {
					t.Fatal(err)
				}
				var target any = sid
				if tc.user {
					target = userID
				}
				if _, err := store.Pool.Exec(ctx, tc.query, target); err != nil {
					t.Fatal(err)
				}
				var result *session.EmailChangeChallengeConsumeResult
				var err error
				if stage == "NewAddress" {
					result, err = store.Impl().ConsumeEmailChangeChallenge(ctx, &dbgen.ConsumeEmailChangeChallengeParams{
						SessionID:      sid,
						ChallengeCode:  db.Text("ABC123"),
						ChallengeEmail: db.Text(newEmail),
						MaxAttempts:    5,
					})
				} else {
					result, err = store.Impl().AdvanceEmailChangeChallenge(ctx, &dbgen.AdvanceEmailChangeChallengeParams{
						SessionID:     sid,
						ChallengeCode: db.Text("ABC123"),
						NewEmail:      db.Text(newEmail),
						NewCode:       db.Text("NEW123"),
						ChallengeTtl:  15 * time.Minute,
						MaxAttempts:   5,
					})
				}
				if err != nil || result == nil || result.Outcome == session.TransitionSucceeded {
					t.Fatalf("guarded transition = (%+v, %v)", result, err)
				}
				var version int32
				if err := store.Pool.QueryRow(ctx, "SELECT version FROM backend.sessions WHERE session_id = $1", sid).Scan(&version); err != nil {
					t.Fatal(err)
				}
				wantVersion := int32(1)
				if stage == "NewAddress" && tc.name == "ChangedRecipient" {
					wantVersion = 2
					if result.Outcome != session.TransitionCancelled || result.Email != "" {
						t.Fatalf("modified destination result = %+v", result)
					}
				}
				if version != wantVersion {
					t.Fatalf("transition version = %d, want %d", version, wantVersion)
				}
			})
		}
	}
}

type failingEmailChangeMailer struct {
	common.Mailer
}

func (f failingEmailChangeMailer) SendTwoFactor(context.Context, string, string, string, string, bool) error {
	return errFromFailingSender
}

func TestEmailChangeDestinationTamperingCancels(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	newEmail := "pending@privatecaptcha.com"
	for _, tc := range []struct {
		name  string
		email string
		code  string
	}{
		{
			name:  "DifferentAddress",
			email: "other@privatecaptcha.com",
			code:  "ABC123",
		},
		{
			name:  "InvalidAddress",
			email: "not-an-email",
			code:  "ABC123",
		},
		{
			name: "EmptyAddress",
			code: "ABC123",
		},
		{
			name:  "ChangedCase",
			email: strings.ToUpper(newEmail),
			code:  "ABC123",
		},
		{
			name:  "AddedWhitespace",
			email: " " + newEmail + " ",
			code:  "ABC123",
		},
		{
			name:  "IncorrectCode",
			email: "other@privatecaptcha.com",
			code:  "WRONG1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			prefix := transitionTestPrefix(t)
			userID, oldEmail := transitionTestUser(t, prefix)
			sid := prefix + "-session"
			if _, err := store.Pool.Exec(ctx, `
				INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at,
				    challenge_kind, challenge_code, challenge_email, challenge_expires_at, failed_attempts)
				VALUES ($1, 'authenticated', $2, '', NOW() + INTERVAL '3 hours',
				    'email_change_new', 'ABC123', $3, NOW() + INTERVAL '15 minutes', 2)
			`, sid, userID, newEmail); err != nil {
				t.Fatal(err)
			}
			primary := db.NewSessionStore(store, server.Metrics)
			secondary := db.NewSessionStore(store, server.Metrics)
			t.Cleanup(primary.Stop)
			t.Cleanup(secondary.Stop)
			if _, err := secondary.Resolve(ctx, sid); err != nil {
				t.Fatal(err)
			}
			submit := func(sessionStore *db.SessionStore, address, code string) *settingsGeneralRenderContext {
				t.Helper()
				originalManager := server.Sessions
				manager := *originalManager
				manager.Store = sessionStore
				server.Sessions = &manager
				defer func() { server.Sessions = originalManager }()
				form := url.Values{
					common.ParamEmail:            {address},
					common.ParamVerificationCode: {code},
					common.ParamVerifyNewEmail:   {"true"},
				}
				req := httptest.NewRequest(http.MethodPut, "/settings/tab/general", strings.NewReader(form.Encode()))
				req.AddCookie(&http.Cookie{
					Name:  manager.CookieName,
					Value: sid,
				})
				req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
				view, err := server.putGeneralSettings(httptest.NewRecorder(), req)
				if err != nil {
					t.Fatal(err)
				}
				if len(view.AuditEvents) != 0 {
					t.Fatal("cancelled email change emitted an update audit event")
				}
				return view.Model.(*settingsGeneralRenderContext)
			}
			model := submit(primary, tc.email, tc.code)
			if model.ErrorMessage != "Email change cancelled. Please start again." || model.EditEmail || model.VerifyNewEmail || model.Email != oldEmail {
				t.Fatalf("tampered destination did not cancel the flow: %+v", model)
			}
			var cleared bool
			var attempts int32
			if err := store.Pool.QueryRow(ctx, `
				SELECT challenge_kind IS NULL AND challenge_code IS NULL AND challenge_email IS NULL AND challenge_expires_at IS NULL,
				    failed_attempts FROM backend.sessions WHERE session_id = $1
			`, sid).Scan(&cleared, &attempts); err != nil {
				t.Fatal(err)
			}
			if !cleared || attempts != 2 {
				t.Fatalf("cancelled challenge = (cleared %v, attempts %d)", cleared, attempts)
			}
			model = submit(secondary, newEmail, "ABC123")
			if model.TwoFactorError == "" || model.SuccessMessage != "" {
				t.Fatalf("stale replica accepted the cancelled code: %+v", model)
			}
			var currentEmail string
			if err := store.Pool.QueryRow(ctx, "SELECT email FROM backend.users WHERE id = $1", userID).Scan(&currentEmail); err != nil {
				t.Fatal(err)
			}
			if currentEmail != oldEmail {
				t.Fatalf("cancelled change updated email to %q", currentEmail)
			}
		})
	}
}

func TestEmailChangeDeliveryFailureAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := t.Context()
	prefix := transitionTestPrefix(t)
	userID, oldEmail := transitionTestUser(t, prefix)
	sid := prefix + "-session"
	if _, err := store.Pool.Exec(ctx, `
		INSERT INTO backend.sessions (session_id, state, user_id, data, expires_at)
		VALUES ($1, 'authenticated', $2, '', NOW() + INTERVAL '3 hours')
	`, sid, userID); err != nil {
		t.Fatal(err)
	}
	sessionStore := db.NewSessionStore(store, server.Metrics)
	t.Cleanup(sessionStore.Stop)
	originalManager := server.Sessions
	originalMailer := server.Mailer
	t.Cleanup(func() {
		server.Sessions = originalManager
		server.Mailer = originalMailer
	})
	manager := *originalManager
	manager.Store = sessionStore
	mailer := &email.StubMailer{}
	server.Sessions = &manager
	server.Mailer = mailer
	cookie := &http.Cookie{
		Name:  manager.CookieName,
		Value: sid,
	}
	issue := func() string {
		t.Helper()
		req := requestWithSessionCookie(http.MethodPost, "/settings/tab/general/email", cookie)
		if _, err := server.editEmail(httptest.NewRecorder(), req); err != nil {
			t.Fatal(err)
		}
		code, ok := mailer.TwoFactorCode(oldEmail)
		if !ok {
			t.Fatal("current-address code was not sent")
		}
		return code
	}
	newEmail := prefix + "-new@privatecaptcha.com"
	update := func(code string, verifyNew bool) *settingsGeneralRenderContext {
		t.Helper()
		form := url.Values{
			common.ParamEmail:            {newEmail},
			common.ParamVerificationCode: {code},
		}
		if verifyNew {
			form.Set(common.ParamVerifyNewEmail, "true")
		}
		req := httptest.NewRequest(http.MethodPut, "/settings/tab/general", strings.NewReader(form.Encode()))
		req.AddCookie(cookie)
		req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
		view, err := server.putGeneralSettings(httptest.NewRecorder(), req)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.AuditEvents) != 0 {
			t.Fatal("unverified email change emitted an update audit event")
		}
		return view.Model.(*settingsGeneralRenderContext)
	}
	code := issue()
	server.Mailer = failingEmailChangeMailer{
		Mailer: mailer,
	}
	model := update(code, false)
	if model.ErrorMessage == "" || model.SuccessMessage != "" || !model.VerifyNewEmail {
		t.Fatalf("delivery failure model = %+v", model)
	}
	if _, err := store.Pool.Exec(ctx, "UPDATE backend.sessions SET challenge_expires_at = NOW() - INTERVAL '1 second' WHERE session_id = $1", sid); err != nil {
		t.Fatal(err)
	}
	model = update("ABC123", true)
	if model.TwoFactorError != "Code has expired. Cancel and start the email change again." {
		t.Fatalf("expired second challenge error = %q", model.TwoFactorError)
	}
	server.Mailer = mailer
	restartedCode := issue()
	model = update(restartedCode, true)
	if model.TwoFactorError == "" {
		t.Fatal("restart allowed second-step verification without current-address proof")
	}
	var currentEmail string
	if err := store.Pool.QueryRow(ctx, "SELECT email FROM backend.users WHERE id = $1", userID).Scan(&currentEmail); err != nil {
		t.Fatal(err)
	}
	if currentEmail != oldEmail {
		t.Fatalf("failed or expired verification changed email to %q", currentEmail)
	}
}
