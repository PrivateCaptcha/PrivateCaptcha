package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/billing"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/difficulty"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/ratelimit"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/rules"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/medama-io/go-useragent"
)

// stubOwnerSource returns a configurable error (and owner id) from OwnerID, so the
// verifier's late-owner-error classification branch can be exercised in isolation.
type stubOwnerSource struct {
	ownerID int32
	orgID   *int32
	err     error
}

var _ puzzle.OwnerIDSource = (*stubOwnerSource)(nil)

func (s *stubOwnerSource) OwnerID(context.Context, time.Time) (int32, *int32, error) {
	return s.ownerID, s.orgID, s.err
}

// propertyOnlyQuerier returns a fixed, enabled Property for GetPropertyByExternalID and
// falls back to the embedded QuerierStub (which returns its configured Error) for every
// other query. This lets the verify chain reach the postponed API key lookup - which then
// fails with whatever Error the stub is configured to return - without needing a live DB.
type propertyOnlyQuerier struct {
	*db.QuerierStub
	property *dbgen.Property
}

func (q *propertyOnlyQuerier) GetPropertyByExternalID(_ context.Context, _ pgtype.UUID) (*dbgen.Property, error) {
	return q.property, nil
}

func newTestProperty(t *testing.T) *dbgen.Property {
	t.Helper()
	externalID := db.UUIDFromSiteKey("aaaaaaaabbbbccccddddeeeeffff0123")
	return &dbgen.Property{
		ID:               1,
		Name:             t.Name(),
		ExternalID:       externalID,
		Domain:           "example.com",
		Enabled:          true,
		ValidityInterval: puzzle.DefaultValidityPeriod,
	}
}

// newStubbedVerifier builds a Verifier whose salt is initialized and whose Store is wired to
// a stubbed BusinessStore. The stub returns the supplied property for
// GetPropertyByExternalID and the stub's configured Error for anything else (including
// GetAPIKeyByExternalID, which is the postponed lookup performed by apiKeyOwnerSource).
func newStubbedVerifier(t *testing.T, property *dbgen.Property, stubError error) (*Verifier, *db.BusinessStore) {
	t.Helper()
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	querier := &propertyOnlyQuerier{
		QuerierStub: &db.QuerierStub{Error: stubError},
		property:    property,
	}
	store := db.NewBusinessWithQuerier(nil, querier, cache)
	verifier := NewVerifier(testsConfigStore(), store, config.NewStaticValue(common.FingerprintHeaderKey, ""), nil)
	if err := verifier.Update(t.Context()); err != nil {
		t.Fatalf("Failed to update verifier: %v", err)
	}
	return verifier, store
}

// buildSignedPayload constructs a real, signature-verified blake2b wire-format payload
// (solutions + "." + puzzle + "." + signature) bound to the supplied property. The
// signature is computed with the verifier's salt only (NeedsExtraSalt() == false), so the
// property's Salt is never consulted and the puzzle passes verifyPuzzleValid on its own.
func buildSignedPayload(t *testing.T, ctx context.Context, verifier *Verifier, property *dbgen.Property) puzzle.SolutionPayload {
	t.Helper()
	p := puzzle.NewComputePuzzle(puzzle.NextPuzzleID(), property.ExternalID.Bytes, 0)
	if err := p.Init(property.ValidityInterval); err != nil {
		t.Fatalf("Failed to init puzzle: %v", err)
	}
	solutions, err := (&puzzle.ComputeSolver{}).Solve(ctx, p)
	if err != nil {
		t.Fatalf("Failed to solve puzzle: %v", err)
	}
	signed, err := p.Serialize(ctx, verifier.Salt.Value(), nil)
	if err != nil {
		t.Fatalf("Failed to serialize puzzle: %v", err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(solutions.String())
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatalf("Failed to write puzzle payload: %v", err)
	}
	payload, err := verifier.ParseSolutionPayload(ctx, encoded.Bytes())
	if err != nil {
		t.Fatalf("Failed to parse signed payload: %v", err)
	}
	return payload
}

// TestVerifyClassifiesOwnerIDError exercises the late-owner-error branch in Verifier.verify
// across a matrix of OwnerID errors. It locks in the bug fix: only the genuine auth
// sentinels collapse to errPuzzleOwner (so handlers answer 403), while transient and
// wrapped-transient errors propagate as-is (so handlers' default branch answers 5xx).
func TestVerifyClassifiesOwnerIDError(t *testing.T) {
	transientErr := errors.New("simulated connection reset")
	wrappedTransientErr := fmt.Errorf("failed to retrieve API key: %w", transientErr)
	wrappedAuthErr := fmt.Errorf("postponed lookup: %w", errInvalidAPIKey)

	property := newTestProperty(t)
	ctx := t.Context()

	for _, tc := range []struct {
		name     string
		ownerErr error
		wantErr  error
	}{
		{name: "InvalidAPIKeyIsAuth", ownerErr: errInvalidAPIKey, wantErr: errPuzzleOwner},
		{name: "APIKeyScopeIsAuth", ownerErr: errAPIKeyScope, wantErr: errPuzzleOwner},
		{name: "APIKeyNotSetIsAuth", ownerErr: errAPIKeyNotSet, wantErr: errPuzzleOwner},
		{name: "WrappedInvalidAPIKeyIsAuth", ownerErr: wrappedAuthErr, wantErr: errPuzzleOwner},
		{name: "TransientErrorPropagates", ownerErr: transientErr, wantErr: transientErr},
		{name: "DeadlineExceededPropagates", ownerErr: context.DeadlineExceeded, wantErr: context.DeadlineExceeded},
		{name: "WrappedTransientPropagates", ownerErr: wrappedTransientErr, wantErr: wrappedTransientErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier, _ := newStubbedVerifier(t, property, nil)
			payload := buildSignedPayload(t, ctx, verifier, property)
			owner := &stubOwnerSource{err: tc.ownerErr}
			_, _, err := verifier.Verify(ctx, payload, owner, time.Now().UTC())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Verifier.Verify err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == errPuzzleOwner && err != errPuzzleOwner {
				t.Fatalf("Verifier.Verify err = %v, want exactly errPuzzleOwner", err)
			}
			if tc.wantErr != errPuzzleOwner && errors.Is(err, errPuzzleOwner) {
				t.Fatalf("Verifier.Verify err = %v, must not be errPuzzleOwner", err)
			}
		})
	}
}

// TestVerifyPropagatesTransientAPIKeyDBError wires the real apiKeyOwnerSource (not a stub)
// to a BusinessStore whose GetAPIKeyByExternalID fails with a transient error, and asserts
// that Verifier.Verify returns the transient error itself rather than collapsing it into
// errPuzzleOwner. This is the direct regression test for the reported bug.
func TestVerifyPropagatesTransientAPIKeyDBError(t *testing.T) {
	transientErr := errors.New("simulated connection reset")
	property := newTestProperty(t)
	ctx := t.Context()
	verifier, store := newStubbedVerifier(t, property, transientErr)
	payload := buildSignedPayload(t, ctx, verifier, property)

	ownerSource := &apiKeyOwnerSource{Store: store, Auth: nil, scope: dbgen.ApiKeyScopePuzzle}
	ctx = context.WithValue(ctx, common.SecretContextKey, db.UUIDToSecret(*randomUUID()))
	_, _, err := verifier.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if errors.Is(err, errPuzzleOwner) {
		t.Fatalf("Verifier.Verify collapsed a transient DB error into errPuzzleOwner (bug): %v", err)
	}
	if !errors.Is(err, transientErr) {
		t.Fatalf("Verifier.Verify err = %v, want transient %v", err, transientErr)
	}
}

// TestVerifyCollapsesGenuineAPIKeyMissingIntoErrPuzzleOwner covers the "key absent" branch:
// when RetrieveAPIKey reports pgx.ErrNoRows (cached as ErrNegativeCacheHit), apiKeyOwnerSource
// maps it to errInvalidAPIKey, and Verifier.Verify must in turn collapse that into
// errPuzzleOwner so the handlers answer 403. This guards against the fix over-propagating
// and silently turning genuine auth failures into 5xx.
func TestVerifyCollapsesGenuineAPIKeyMissingIntoErrPuzzleOwner(t *testing.T) {
	property := newTestProperty(t)
	ctx := t.Context()
	verifier, store := newStubbedVerifier(t, property, pgx.ErrNoRows)
	payload := buildSignedPayload(t, ctx, verifier, property)

	ownerSource := &apiKeyOwnerSource{Store: store, Auth: nil, scope: dbgen.ApiKeyScopePuzzle}
	ctx = context.WithValue(ctx, common.SecretContextKey, db.UUIDToSecret(*randomUUID()))
	_, _, err := verifier.Verify(ctx, payload, ownerSource, time.Now().UTC())
	if err != errPuzzleOwner {
		t.Fatalf("Verifier.Verify err = %v, want errPuzzleOwner for a missing API key", err)
	}
}

// newStubbedServer builds a Server whose BusinessDB is a stubbed BusinessStore: it returns
// the supplied property for GetPropertyByExternalID and stubError for everything else. The
// returned Server has the verify routes registered through the same middleware chain used
// in production (APIKey auth + SoftTimeoutHandler), so end-to-end error mapping can be
// observed via httptest.
func newStubbedServer(t *testing.T, property *dbgen.Property, stubError error) *Server {
	t.Helper()
	cache := db.NewStaticCache[db.CacheKey, any](1000, &db.CacheMissingValue{})
	querier := &propertyOnlyQuerier{
		QuerierStub: &db.QuerierStub{Error: stubError},
		property:    property,
	}
	store := db.NewBusinessWithQuerier(nil, querier, cache)
	metrics := monitoring.NewStub()
	planService := billing.NewPlanService(nil)
	parser := useragent.NewParser()
	srv := &Server{
		Stage:               common.StageTest,
		BusinessDB:          store,
		TimeSeries:          db.NewMemoryTimeSeries(),
		RateLimiter:         &ratelimit.StubRateLimiter{Header: "X-Forwarded-For"},
		Auth:                NewAuthMiddleware(store, NewUserLimiter(store, planService), planService, metrics, rules.NewRulesCompiler(parser)),
		VerifyLogChan:       make(chan *common.VerifyRecord, 10),
		FormSubmitLogChan:   make(chan *common.FormSubmitRecord, 10),
		FormSubmitLogCancel: func() {},
		FormURLVerifier:     AllowAllFormURLVerifier{},
		Verifier:            NewVerifier(testsConfigStore(), store, config.NewStaticValue(common.FingerprintHeaderKey, "FP"), parser),
		Metrics:             metrics,
		Levels:              difficulty.NewLevels(db.NewMemoryTimeSeries(), 10, PropertyBucketSize),
		VerifyLogCancel:     func() {},
		SubscriptionLimits:  db.NewSubscriptionLimits(common.StageTest, store, planService),
		IDHasher:            common.NewIDHasher(config.NewStaticValue(common.IDHasherSaltKey, "salt")),
		CountryCodeHeader:   config.NewStaticValue(common.CountryCodeHeaderKey, "CF"),
	}
	if err := srv.Verifier.Update(t.Context()); err != nil {
		t.Fatalf("Failed to update verifier: %v", err)
	}
	srv.APIHeaders = make(map[string][]string)
	return srv
}

// httpVerifySuite issues a POST /verify (PrivateCaptcha wire format) against the supplied
// server with the given API key header, sitekey header, and body.
func httpVerifySuite(t *testing.T, srv *Server, body, secret, sitekey string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.Setup("", false, common.NoopMiddleware).Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/"+common.VerifyEndpoint, strings.NewReader(body))
	req.Header.Set(common.HeaderAPIKey, secret)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	if len(sitekey) > 0 {
		req.Header.Set(common.HeaderSitekey, sitekey)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// httpSiteVerifySuite issues a POST /siteverify (reCAPTCHA compatibility) against the
// supplied server with the given secret and response form fields.
func httpSiteVerifySuite(t *testing.T, srv *Server, secret, response, sitekey string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.Setup("", false, common.NoopMiddleware).Register(mux)
	form := url.Values{}
	form.Set(common.ParamSecret, secret)
	form.Set(common.ParamResponse, response)
	if len(sitekey) > 0 {
		form.Set(common.ParamSiteKey, sitekey)
	}
	req := httptest.NewRequest(http.MethodPost, "/"+common.SiteVerifyEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set(common.HeaderContentType, common.ContentTypeURLEncoded)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// buildSignedWirePayload returns the on-the-wire string for a blake2b puzzle bound to the
// given property, signed with the server verifier's salt. Used by the HTTP-level tests.
func buildSignedWirePayload(t *testing.T, srv *Server, property *dbgen.Property) string {
	t.Helper()
	ctx := t.Context()
	p := puzzle.NewComputePuzzle(puzzle.NextPuzzleID(), property.ExternalID.Bytes, 0)
	if err := p.Init(property.ValidityInterval); err != nil {
		t.Fatal(err)
	}
	solutions, err := (&puzzle.ComputeSolver{}).Solve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := p.Serialize(ctx, srv.Verifier.Salt.Value(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	encoded.WriteString(solutions.String())
	encoded.WriteByte('.')
	if err := signed.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	return encoded.String()
}

// TestVerifyHandlerTransientAPIKeyDBErrorReturns500 is the end-to-end regression test: a
// legitimate wire-format payload is signed for a real property, the request carries a
// well-formed API key header, but GetAPIKeyByExternalID fails with a transient error on
// the postponed lookup. Before the fix this returned 403; after the fix it MUST return 5xx
// (the handlers' default branch), matching how the AuthMiddleware classifies the same error.
func TestVerifyHandlerTransientAPIKeyDBErrorReturns500(t *testing.T) {
	transientErr := errors.New("simulated connection reset")
	property := newTestProperty(t)
	srv := newStubbedServer(t, property, transientErr)
	secret := db.UUIDToSecret(*randomUUID())
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	w := httpVerifySuite(t, srv, buildSignedWirePayload(t, srv, property), secret, sitekey)
	if w.Code == http.StatusForbidden {
		t.Fatalf("BUG: /verify returned 403 on a transient API key DB error: %v", w.Code)
	}
	if w.Code < 500 {
		t.Fatalf("/verify status = %d, want 5xx for a transient API key DB error", w.Code)
	}
}

// TestSiteVerifyHandlerTransientAPIKeyDBErrorReturns500 mirrors the above for the
// reCAPTCHA-compatibility /siteverify handler, which replicates the same errPuzzleOwner
// switch and therefore must also answer 5xx (not 403) on a transient API key DB error.
func TestSiteVerifyHandlerTransientAPIKeyDBErrorReturns500(t *testing.T) {
	transientErr := errors.New("simulated connection reset")
	property := newTestProperty(t)
	srv := newStubbedServer(t, property, transientErr)
	secret := db.UUIDToSecret(*randomUUID())
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	wire := buildSignedWirePayload(t, srv, property)
	w := httpSiteVerifySuite(t, srv, secret, wire, sitekey)
	if w.Code == http.StatusForbidden {
		t.Fatalf("BUG: /siteverify returned 403 on a transient API key DB error: %v", w.Code)
	}
	if w.Code < 500 {
		t.Fatalf("/siteverify status = %d, want 5xx for a transient API key DB error", w.Code)
	}
}

// TestVerifyHandlerGenuineAuthErrorReturns403 guards the opposite direction: a genuinely
// missing API key (pgx.ErrNoRows -> ErrNegativeCacheHit -> errInvalidAPIKey ->
// errPuzzleOwner) MUST still answer 403, so the fix does not regress genuine auth failures
// into 5xx.
func TestVerifyHandlerGenuineAuthErrorReturns403(t *testing.T) {
	property := newTestProperty(t)
	srv := newStubbedServer(t, property, pgx.ErrNoRows)
	secret := db.UUIDToSecret(*randomUUID())
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	w := httpVerifySuite(t, srv, buildSignedWirePayload(t, srv, property), secret, sitekey)
	if w.Code != http.StatusForbidden {
		t.Fatalf("/verify status = %d, want 403 for a genuinely missing API key", w.Code)
	}
}

// TestSiteVerifyHandlerGenuineAuthErrorReturns403 mirrors the above for /siteverify.
func TestSiteVerifyHandlerGenuineAuthErrorReturns403(t *testing.T) {
	property := newTestProperty(t)
	srv := newStubbedServer(t, property, pgx.ErrNoRows)
	secret := db.UUIDToSecret(*randomUUID())
	sitekey := db.UUIDToSiteKey(property.ExternalID)
	w := httpSiteVerifySuite(t, srv, secret, buildSignedWirePayload(t, srv, property), sitekey)
	if w.Code != http.StatusForbidden {
		t.Fatalf("/siteverify status = %d, want 403 for a genuinely missing API key", w.Code)
	}
}
