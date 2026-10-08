//go:build enterprise

package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/golang-jwt/jwt/v5"
)

type edgeTestConfigItem struct {
	key   common.ConfigKey
	value atomic.Value
}

func (c *edgeTestConfigItem) Key() common.ConfigKey { return c.key }
func (c *edgeTestConfigItem) Value() string         { return c.value.Load().(string) }
func newEdgeTestConfigItem(key common.ConfigKey, value string) *edgeTestConfigItem {
	c := &edgeTestConfigItem{key: key}
	c.value.Store(value)
	return c
}

func newEdgeTestConfig(items ...common.ConfigItem) common.ConfigStore {
	cfg := config.NewBaseConfig(config.NewEnvConfig(func(string) string { return "" }))
	for _, item := range items {
		cfg.Add(item)
	}
	return cfg
}

func TestEdgeTokenSecondaryPrepublication(t *testing.T) {
	private, public := newTestEdgeKeyPair(t)
	_, secondaryPublic := newTestEdgeKeyPair(t)
	signer := NewEdgeTokenSigner("", newEdgeTestConfig(
		newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, private),
		newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, public),
		newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, secondaryPublic),
	))
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	configured, err := newEdgeTokenSignerFromStrings(t.Context(), "", private, public)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
	if err != nil || !verifyTestEdgeSignature(configured.JWKS(t.Context()).Keys[0], token) {
		t.Fatalf("prepublishing a secondary key changed the configured signing key: %v", err)
	}
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 2 {
		t.Fatalf("secondary key was not published: %+v", keys)
	}
}

func TestEdgeTokenKeysReload(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, firstPublic)
	secondary := newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, "")
	signer := NewEdgeTokenSigner("", newEdgeTestConfig(private, public, secondary))
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	firstKid := signer.JWKS(t.Context()).Keys[0].Kid
	secondary.value.Store(secondPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 2 || keys[0].Kid != firstKid || keys[1].Kid == firstKid {
		t.Fatalf("rotated key not staged alongside current key: %+v", keys)
	}
	public.value.Store(secondPublic)
	if err := signer.Update(t.Context()); err == nil {
		t.Fatal("mismatched reloaded keys accepted")
	}
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 2 || keys[0].Kid != firstKid || keys[1].Kid == firstKid {
		t.Fatalf("invalid reload lost staged rotation: %+v", keys)
	}
	token, err := signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
	if err != nil || !verifyTestEdgeSignature(signer.JWKS(t.Context()).Keys[0], token) {
		t.Fatalf("invalid reload interrupted signing: %v", err)
	}
	private.value.Store(secondPrivate)
	secondary.value.Store(firstPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	keys := signer.JWKS(t.Context()).Keys
	if len(keys) != 2 || keys[1].Kid != firstKid {
		t.Fatalf("activated rotation did not retain secondary key: %+v", keys)
	}
	public.value.Store(firstPublic)
	if err := signer.Update(t.Context()); err == nil {
		t.Fatal("mismatched reload after activation accepted")
	}
	if got := signer.JWKS(t.Context()).Keys; len(got) != 2 || got[0].Kid != keys[0].Kid || got[1].Kid != firstKid {
		t.Fatalf("invalid reload lost configured secondary key: %+v", got)
	}
	token, err = signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
	if err != nil || !verifyTestEdgeSignature(keys[0], token) {
		t.Fatalf("invalid reload interrupted activated signer: %v", err)
	}
	private.value.Store("")
	public.value.Store("")
	secondary.value.Store("")
	if err := signer.Update(t.Context()); err != nil || len(signer.JWKS(t.Context()).Keys) != 0 {
		t.Fatalf("explicitly disabling keys did not clear state: %v", err)
	}
	w := httptest.NewRecorder()
	(&Server{EdgeTokens: signer}).edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if w.Body.String() != `{"keys":[]}` || w.Header().Get(common.HeaderCacheControl) != "no-store" {
		t.Fatalf("disabled JWKS can be cached across enablement: %s %v", w.Body.String(), w.Header())
	}
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	(&Server{EdgeTokens: signer}).edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if w.Header().Get(common.HeaderCacheControl) != "public, max-age=300" {
		t.Fatalf("enabled JWKS lost caching: %v", w.Header())
	}
	var enabled EdgeJWKS
	if err := json.Unmarshal(w.Body.Bytes(), &enabled); err != nil {
		t.Fatal(err)
	}
	token, err = signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
	if err != nil || len(enabled.Keys) != 1 || !verifyTestEdgeSignature(enabled.Keys[0], token) {
		t.Fatalf("re-enabled signer did not publish its signing key: %v", err)
	}
}

func TestEdgeTokenReloadRetainsOutgoingKey(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, firstPublic)
	signer := NewEdgeTokenSigner("", newEdgeTestConfig(private, public))
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := signer.JWKS(t.Context()).Keys[0]
	issuedAt := time.Now()
	oldToken, err := signer.Sign(t.Context(), "sitekey", "example.com", issuedAt, maxEdgeTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	before := time.Now()
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	keys := signer.JWKS(t.Context()).Keys
	if len(keys) != 2 || keys[0].Kid == first.Kid || keys[1].Kid != first.Kid {
		t.Fatalf("live reload did not activate the configured key and retain the outgoing key: %+v", keys)
	}
	expiresAt := signer.state.retired[0].expiresAt
	if expiresAt.Before(before.Add(maxEdgeTokenTTL)) {
		t.Fatal("outgoing key retention is shorter than the maximum token lifetime")
	}
	token, err := signer.Sign(t.Context(), "sitekey", "example.com", issuedAt, maxEdgeTokenTTL)
	if err != nil || !verifyTestEdgeSignature(keys[0], token) {
		t.Fatalf("live reload did not use the configured signing key: %v", err)
	}
	if _, err := signer.Sign(t.Context(), "sitekey", "example.com", issuedAt, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(signer.state.retired) != 1 || !signer.state.retired[0].expiresAt.Equal(expiresAt) {
		t.Fatal("reloading the same signing key changed outgoing key retention")
	}
	keys = signer.JWKS(t.Context()).Keys
	if len(keys) != 2 || keys[1].Kid != first.Kid || !verifyTestEdgeSignature(keys[1], oldToken) {
		t.Fatalf("live reload dropped the key for an unexpired token: %+v", keys)
	}
	signer.mu.Lock()
	signer.pruneRetiredLocked(expiresAt.Add(-time.Second))
	signer.mu.Unlock()
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 2 || keys[1].Kid != first.Kid {
		t.Fatalf("old key was removed before the longest-lived token expired: %+v", keys)
	}
	signer.mu.Lock()
	signer.pruneRetiredLocked(expiresAt)
	signer.mu.Unlock()
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 1 || keys[0].Kid == first.Kid {
		t.Fatalf("old key was not removed after token expiry: %+v", keys)
	}
}

func TestEdgeTokenRotationRetainsLatestTokenExpiry(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, firstPublic)
	signer := NewEdgeTokenSigner("", newEdgeTestConfig(private, public))
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	firstKid := signer.JWKS(t.Context()).Keys[0].Kid
	verificationKey, err := jwt.ParseECPublicKeyFromPEM([]byte(firstPublic))
	if err != nil {
		t.Fatal(err)
	}
	claimsTime := time.Now().Add(2 * time.Hour).UTC()
	oldToken, err := signer.Sign(t.Context(), "sitekey", "example.com", claimsTime, maxEdgeTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(t.Context(), "sitekey", "example.com", claimsTime.Add(time.Minute), time.Hour); err != nil {
		t.Fatal(err)
	}
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	retiredAt := time.Now()
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	newToken, err := signer.Sign(t.Context(), "sitekey", "example.com", claimsTime.Add(2*time.Hour), maxEdgeTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := claimsTime.Truncate(time.Second).Add(maxEdgeTokenTTL)
	for _, checkAt := range []time.Time{retiredAt.Add(maxEdgeTokenTTL), expiresAt.Add(-time.Second)} {
		signer.mu.Lock()
		signer.pruneRetiredLocked(checkAt)
		signer.mu.Unlock()
		keys := signer.JWKS(t.Context()).Keys
		parsed, err := jwt.Parse(oldToken, func(token *jwt.Token) (any, error) {
			for _, key := range keys {
				if key.Kid == firstKid && token.Header["kid"] == key.Kid {
					return verificationKey, nil
				}
			}
			return nil, errEdgeKeysUnavailable
		}, jwt.WithTimeFunc(func() time.Time {
			return checkAt
		}), jwt.WithIssuedAt(), jwt.WithValidMethods([]string{edgeKeyAlgorithm}), jwt.WithAudience("sitekey"), jwt.WithIssuer(edgeDefaultAuthority))
		if err != nil || !parsed.Valid {
			t.Fatalf("unexpired token failed JWKS verification at %s: %v", checkAt, err)
		}
	}
	signer.mu.Lock()
	signer.pruneRetiredLocked(expiresAt)
	signer.mu.Unlock()
	keys := signer.JWKS(t.Context()).Keys
	if len(keys) != 1 || keys[0].Kid == firstKid || !verifyTestEdgeSignature(keys[0], newToken) {
		t.Fatalf("token expiry did not remove only the retired key: %+v", keys)
	}
}

func TestEdgeTokenConsecutiveRotationsRetainUnexpiredKeys(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, firstPublic)
	secondary := newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, "")
	signer := NewEdgeTokenSigner("", newEdgeTestConfig(private, public, secondary))
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Now()
	firstToken, err := signer.Sign(t.Context(), "sitekey", "example.com", issuedAt, maxEdgeTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	first := signer.JWKS(t.Context()).Keys[0]
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	secondary.value.Store(firstPublic)
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	firstExpiresAt := signer.state.retired[0].expiresAt
	secondToken, err := signer.Sign(t.Context(), "sitekey", "example.com", issuedAt.Add(time.Hour), maxEdgeTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	second := signer.JWKS(t.Context()).Keys[0]
	thirdPrivate, thirdPublic := newTestEdgeKeyPair(t)
	secondary.value.Store(secondPublic)
	private.value.Store(thirdPrivate)
	public.value.Store(thirdPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	secondExpiresAt := signer.state.retired[1].expiresAt
	secondary.value.Store("")
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	keys := signer.JWKS(t.Context()).Keys
	if len(keys) != 3 || keys[1].Kid != first.Kid || keys[2].Kid != second.Kid ||
		!verifyTestEdgeSignature(keys[1], firstToken) || !verifyTestEdgeSignature(keys[2], secondToken) {
		t.Fatalf("consecutive rotations lost unexpired verification keys: %+v", keys)
	}
	signer.mu.Lock()
	signer.pruneRetiredLocked(firstExpiresAt)
	signer.mu.Unlock()
	keys = signer.JWKS(t.Context()).Keys
	if len(keys) != 2 || keys[1].Kid != second.Kid || !verifyTestEdgeSignature(keys[1], secondToken) {
		t.Fatalf("expiring the first key lost the more recently retired second key: %+v", keys)
	}
	signer.mu.Lock()
	signer.pruneRetiredLocked(secondExpiresAt)
	signer.mu.Unlock()
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 1 || keys[0].Kid == first.Kid || keys[0].Kid == second.Kid {
		t.Fatalf("expired keys remain after consecutive rotations: %+v", keys)
	}
}

func TestEdgeTokenRestartRotation(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	oldSigner, err := newEdgeTokenSignerFromStrings(t.Context(), "", firstPrivate, firstPublic)
	if err != nil {
		t.Fatal(err)
	}
	oldToken, err := oldSigner.Sign(t.Context(), "sitekey", "example.com", time.Now(), maxEdgeTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	signer := NewEdgeTokenSigner("", newEdgeTestConfig(
		newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, secondPrivate),
		newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, secondPublic),
		newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, firstPublic),
	))
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	keys := signer.JWKS(t.Context()).Keys
	if len(keys) != 2 || keys[0].Kid == keys[1].Kid || !verifyTestEdgeSignature(keys[1], oldToken) {
		t.Fatalf("restart did not retain the configured verification key: %+v", keys)
	}
	token, err := signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
	if err != nil || !verifyTestEdgeSignature(keys[0], token) || verifyTestEdgeSignature(keys[1], token) {
		t.Fatalf("restart did not use the configured signing key immediately: %v", err)
	}
}

func TestEdgeTokenSecondaryPublicKey(t *testing.T) {
	private, public := newTestEdgeKeyPair(t)
	_, otherPublic := newTestEdgeKeyPair(t)
	otherCurve, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherCurveDER, err := x509.MarshalPKIXPublicKey(&otherCurve.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	otherCurvePublic := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: otherCurveDER,
	}))
	for _, tc := range []struct {
		name          string
		initialPublic string
		public        string
		wantErr       error
		count         int
	}{
		{
			name:    "invalid PEM",
			public:  "invalid",
			wantErr: errEdgeInvalidPublicKey,
			count:   1,
		},
		{
			name:    "unsupported curve",
			public:  otherCurvePublic,
			wantErr: errEdgeInvalidKeyCurve,
			count:   1,
		},
		{
			name:   "same as signing key",
			public: public,
			count:  1,
		},
		{
			name:   "verification only",
			public: otherPublic,
			count:  2,
		},
		{
			name:          "invalid reload preserves secondary",
			initialPublic: otherPublic,
			public:        "invalid",
			wantErr:       errEdgeInvalidPublicKey,
			count:         2,
		},
		{
			name:          "removed",
			initialPublic: otherPublic,
			count:         1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secondary := newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, tc.initialPublic)
			signer := NewEdgeTokenSigner("", newEdgeTestConfig(
				newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, private),
				newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, public),
				secondary,
			))
			if err := signer.Update(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := signer.JWKS(t.Context()).Keys
			signing := before[0]
			secondary.value.Store(tc.public)
			if err := signer.Update(t.Context()); err != tc.wantErr {
				t.Fatalf("secondary public key error = %v, want %v", err, tc.wantErr)
			}
			keys := signer.JWKS(t.Context()).Keys
			if len(keys) != tc.count || keys[0].Kid != signing.Kid {
				t.Fatalf("unexpected published keys: %+v", keys)
			}
			if tc.wantErr != nil && !slices.Equal(keys, before) {
				t.Fatalf("invalid reload changed published keys: before=%+v after=%+v", before, keys)
			}
			token, err := signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
			if err != nil || !verifyTestEdgeSignature(signing, token) {
				t.Fatalf("secondary public key reload interrupted signing: %v", err)
			}
		})
	}
}

func TestEdgeTokenRotationIgnoresClaimsTime(t *testing.T) {
	for _, tc := range []struct {
		name         string
		claimsOffset time.Duration
	}{
		{
			name:         "future claims",
			claimsOffset: 2 * time.Hour,
		},
		{
			name:         "past claims",
			claimsOffset: -2 * time.Hour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, secondaryPublic := newTestEdgeKeyPair(t)
			private, public := newTestEdgeKeyPair(t)
			signer := NewEdgeTokenSigner("", newEdgeTestConfig(
				newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, private),
				newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, public),
				newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, secondaryPublic),
			))
			if err := signer.Update(t.Context()); err != nil {
				t.Fatal(err)
			}
			keys := signer.JWKS(t.Context()).Keys
			claimsTime := time.Now().Add(tc.claimsOffset).UTC()
			token, err := signer.Sign(t.Context(), "sitekey", "example.com", claimsTime, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if !verifyTestEdgeSignature(keys[0], token) || verifyTestEdgeSignature(keys[1], token) {
				t.Fatal("caller claims time changed the configured signing key")
			}
			payload, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[1])
			if err != nil {
				t.Fatal(err)
			}
			var claims map[string]any
			if err := json.Unmarshal(payload, &claims); err != nil {
				t.Fatal(err)
			}
			if claims["iat"] != float64(claimsTime.Unix()) || claims["exp"] != float64(claimsTime.Add(time.Hour).Unix()) {
				t.Fatalf("token claims did not retain caller time: %v", claims)
			}
		})
	}
}

func TestEdgeTokenTwoPhaseRotation(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, firstPublic)
	secondary := newEdgeTestConfigItem(common.EdgeTokenSecondaryPublicKeyKey, "")
	cfg := newEdgeTestConfig(private, public, secondary)
	replicas := []*EdgeTokenSigner{NewEdgeTokenSigner("", cfg), NewEdgeTokenSigner("", cfg)}
	for _, replica := range replicas {
		if err := replica.Update(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	oldOnlyCache := replicas[1].JWKS(t.Context()).Keys[0]
	secondary.value.Store(secondPublic)
	if err := replicas[0].Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	staged := replicas[0].JWKS(t.Context()).Keys
	if len(staged) != 2 || staged[0].Kid != oldOnlyCache.Kid || staged[1].Kid == oldOnlyCache.Kid {
		t.Fatalf("rotation must publish both keys before switching: %+v", staged)
	}
	w := httptest.NewRecorder()
	(&Server{EdgeTokens: replicas[0]}).edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	var published EdgeJWKS
	if err := json.Unmarshal(w.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if len(published.Keys) != 2 || published.Keys[0] != staged[0] || published.Keys[1] != staged[1] {
		t.Fatalf("JWKS endpoint did not prepublish replacement key: %+v", published.Keys)
	}
	for _, replica := range replicas {
		token, err := replica.Sign(t.Context(), "sitekey", "example.com", time.Now(), maxEdgeTokenTTL)
		if err != nil || !verifyTestEdgeSignature(oldOnlyCache, token) {
			t.Fatalf("publish rollout issued a token absent from the old-only cache: %v", err)
		}
	}
	if err := replicas[1].Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, replica := range replicas {
		keys := replica.JWKS(t.Context()).Keys
		if len(keys) != 2 || keys[0] != staged[0] || keys[1] != staged[1] {
			t.Fatalf("publish rollout did not converge: %+v", keys)
		}
	}

	// Activation is a separate rollout after old-only caches have expired.
	// Keep the dual-key cache fixed while replicas activate at different times.
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	secondary.value.Store(firstPublic)
	if err := replicas[0].Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, replica := range replicas {
		token, err := replica.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
		key := published.Keys[1-i]
		if err != nil || !verifyTestEdgeSignature(key, token) {
			t.Fatalf("replica %d issued a token absent from the dual-key cache: %v", i, err)
		}
	}
	if err := replicas[1].Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	replicas = append(replicas, NewEdgeTokenSigner("", cfg))
	if err := replicas[2].Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, replica := range replicas {
		keys := replica.JWKS(t.Context()).Keys
		if len(keys) != 2 || keys[0] != staged[1] || keys[1] != staged[0] {
			t.Fatalf("activation rollout or restart lost published keys: %+v", keys)
		}
		token, err := replica.Sign(t.Context(), "sitekey", "example.com", time.Now(), time.Hour)
		if err != nil || !verifyTestEdgeSignature(published.Keys[1], token) {
			t.Fatalf("activation rollout or restart did not use the configured signing key: %v", err)
		}
	}
	secondary.value.Store("")
	if err := replicas[0].Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if keys := replicas[0].JWKS(t.Context()).Keys; len(keys) != 2 || keys[0] != staged[1] || keys[1] != staged[0] {
		t.Fatalf("live reload dropped an unexpired outgoing verification key: %+v", keys)
	}
}

func TestExampleEdgeTokenKeys(t *testing.T) {
	path := "docker/pc.env.example"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = "../../docker/pc.env.example"
	}
	env, err := common.NewEnvMap(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.NewEnvConfig(env.Get)
	signer := NewEdgeTokenSigner("", cfg)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(signer.JWKS(t.Context()).Keys) != 1 {
		t.Fatal("example edge keys are not a valid pair")
	}
}

func TestEdgeTokenPropertyDomain(t *testing.T) {
	private, public := newTestEdgeKeyPair(t)
	signer, err := newEdgeTokenSignerFromStrings(t.Context(), "", private, public)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"", "bad host", "localhost", "127.0.0.1"} {
		if err := signer.ValidateProperty(t.Context(), domain, time.Hour); err == nil {
			t.Errorf("accepted invalid property domain %q", domain)
		}
	}
	if err := signer.ValidateProperty(t.Context(), "example.com", time.Hour); err != nil {
		t.Fatalf("rejected valid property domain: %v", err)
	}
	if err := signer.ValidateProperty(t.Context(), "", 0); err != nil {
		t.Fatalf("rejected disabled edge with no property domain: %v", err)
	}
}

func TestValidatePropertyKeysNotConfigured(t *testing.T) {
	signer, err := newEdgeTokenSignerFromStrings(t.Context(), "", "", "")
	if err != nil {
		t.Fatalf("failed to create signer with empty config: %v", err)
	}
	if err := signer.ValidateLifetime(t.Context(), time.Hour); err != errEdgeInvalidLifetime {
		t.Fatalf("ValidateLifetime with keys absent: got err = %q, want errEdgeInvalidLifetime = %q", err, errEdgeInvalidLifetime)
	}
	err = signer.ValidateProperty(t.Context(), "freya.example.com", time.Hour)
	if err == nil {
		t.Fatal("expected error when keys not configured, got nil")
	}
	if err != errEdgeInvalidLifetime {
		t.Fatalf("ValidateProperty with valid domain and keys absent: got err = %q, want errEdgeInvalidLifetime = %q (not errEdgeInvalidDomain = %q)",
			err, errEdgeInvalidLifetime, errEdgeInvalidDomain)
	}
	if err == errEdgeInvalidDomain {
		t.Fatal("domain error reported even when keys are absent and the domain is valid; bug confirmed")
	}
}

func TestEdgeTokenKeysAndClaims(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	otherPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPrivateDER, err := x509.MarshalPKCS8PrivateKey(otherPrivate)
	if err != nil {
		t.Fatal(err)
	}
	otherPublicDER, err := x509.MarshalPKIXPublicKey(&otherPrivate.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	otherPrivatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: otherPrivateDER}))
	otherPublicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: otherPublicDER}))

	const myAuthority = "https://justfortest.com"

	for _, tt := range []struct {
		name, priv, pub string
		wantErr         error
	}{
		{"absent", "", "", nil},
		{"private only", privatePEM, "", errEdgeKeysUnpaired},
		{"public only", "", publicPEM, errEdgeKeysUnpaired},
		{"invalid private", "invalid", publicPEM, errEdgeInvalidPrivateKey},
		{"invalid public", privatePEM, "invalid", errEdgeInvalidPublicKey},
		{"mismatch", privatePEM, otherPublicPEM, errEdgeKeyMismatch},
		{"paired", privatePEM, publicPEM, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := newEdgeTokenSignerFromStrings(t.Context(), myAuthority, tt.priv, tt.pub)
			if err != tt.wantErr {
				t.Fatalf("signer=%v, error=%v, want error=%v", signer, err, tt.wantErr)
			}
			if tt.name != "paired" {
				return
			}
			jwks := signer.JWKS(t.Context())
			if len(jwks.Keys) != 1 || jwks.Keys[0].Kty != "EC" || jwks.Keys[0].Crv != "P-256" || jwks.Keys[0].Alg != "ES256" {
				t.Fatalf("invalid JWKS: %+v", jwks)
			}
			fingerprint := sha256.Sum256(publicDER)
			if jwks.Keys[0].Kid != base64.RawURLEncoding.EncodeToString(fingerprint[:]) {
				t.Fatalf("kid does not match public key fingerprint: %s", jwks.Keys[0].Kid)
			}
			now := time.Unix(1700000000, 0)
			token, err := signer.Sign(t.Context(), "sitekey-one", "example.com", now, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				t.Fatalf("invalid compact JWS: %s", token)
			}
			decode := func(s string) map[string]any {
				data, err := base64.RawURLEncoding.DecodeString(s)
				if err != nil {
					t.Fatal(err)
				}
				var m map[string]any
				if err := json.Unmarshal(data, &m); err != nil {
					t.Fatal(err)
				}
				return m
			}
			header, claims := decode(parts[0]), decode(parts[1])
			if header["alg"] != "ES256" || header["typ"] != "pc-edge+jwt" || header["kid"] != jwks.Keys[0].Kid {
				t.Fatalf("header: %v", header)
			}
			if claims["v"] != float64(1) || claims["iss"] != myAuthority || claims["aud"] != "sitekey-one" || claims["host"] != "example.com" ||
				claims["iat"] != float64(now.Unix()) ||
				claims["exp"] != float64(now.Add(time.Hour).Unix()) {
				t.Fatalf("claims: %v", claims)
			}
			if !verifyTestEdgeSignature(jwks.Keys[0], token) {
				t.Fatal("signature did not verify")
			}
			other, err := newEdgeTokenSignerFromStrings(t.Context(), myAuthority, otherPrivatePEM, otherPublicPEM)
			if err != nil {
				t.Fatal(err)
			}
			if verifyTestEdgeSignature(other.JWKS(t.Context()).Keys[0], token) {
				t.Fatal("other key verified signature")
			}
		})
	}
}

func newTestEdgeKeyPair(t *testing.T) (string, string) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
}

func newEdgeTokenSignerFromStrings(ctx context.Context, authority, private, public string) (*EdgeTokenSigner, error) {
	signer := NewEdgeTokenSigner(authority, newEdgeTestConfig(newEdgeTestConfigItem(common.EdgeTokenSigningPrivateKeyKey, private), newEdgeTestConfigItem(common.EdgeTokenSigningPublicKeyKey, public)))
	if err := signer.Update(ctx); err != nil {
		return nil, err
	}
	return signer, nil
}

func newTestEdgeSigner(t *testing.T) *EdgeTokenSigner {
	t.Helper()
	private, public := newTestEdgeKeyPair(t)
	signer, err := newEdgeTokenSignerFromStrings(t.Context(), "", private, public)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func verifyTestEdgeSignature(jwk edgeJWK, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	decode := func(s string) []byte { result, _ := base64.RawURLEncoding.DecodeString(s); return result }
	sig := decode(parts[2])
	if len(sig) != 64 {
		return false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(decode(jwk.X)), Y: new(big.Int).SetBytes(decode(jwk.Y))}
	return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

func TestEdgeTokenOnlyOnSuccessfulVerification(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(private)
	publicDER, _ := x509.MarshalPKIXPublicKey(&private.PublicKey)
	signer, err := newEdgeTokenSignerFromStrings(
		t.Context(),
		"",
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
	)
	if err != nil {
		t.Fatal(err)
	}
	cache := db.NewStaticCache[db.CacheKey, any](10, &db.CacheMissingValue{})
	if err := cache.SetMissing(t.Context(), db.CompiledPropertyRulesCacheKey(1)); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		EdgeTokens: signer,
		BusinessDB: db.NewBusinessEx(nil, cache),
	}
	now := time.Unix(1700000000, 0)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/verify", nil)
	property := &dbgen.Property{
		ID:                        1,
		ExternalID:                db.UUIDFromSiteKey("11111111222233334444555555555555"),
		Domain:                    "example.com",
		EdgeTokenValidityInterval: time.Hour,
	}
	result := &puzzle.VerifyResult{
		Error:  puzzle.VerifyNoError,
		Domain: property.Domain,
	}
	response := newVerificationResponse(result, false)
	if err := s.addEdgeToken(req, response, result, property, now); err != nil {
		t.Fatal(err)
	}
	if response.EdgeToken == "" || !verifyTestEdgeSignature(signer.JWKS(t.Context()).Keys[0], response.EdgeToken) {
		t.Fatal("successful verification must return signed token")
	}
	result.Error = puzzle.VerifiedBeforeError
	response = newVerificationResponse(result, false)
	if err := s.addEdgeToken(req, response, result, property, now); err != nil || response.EdgeToken != "" {
		t.Fatalf("failed verification returned token: %v, %v", response.EdgeToken, err)
	}
	result.Error = puzzle.VerifyNoError
	property.EdgeTokenValidityInterval = 0
	response = newVerificationResponse(result, false)
	if err := s.addEdgeToken(req, response, result, property, now); err != nil || response.EdgeToken != "" {
		t.Fatalf("disabled setting returned token: %v, %v", response.EdgeToken, err)
	}
	result.Error = puzzle.MaintenanceModeError
	property.EdgeTokenValidityInterval = time.Hour
	response = newVerificationResponse(result, false)
	if err := s.addEdgeToken(req, response, result, property, now); err != nil || !response.Success || response.EdgeToken == "" {
		t.Fatalf("maintenance with edge property metadata did not sign: response=%+v err=%v", response, err)
	}
	result.Error = puzzle.VerifyNoError
	s.EdgeTokens = nil
	if err := s.addEdgeToken(req, response, result, property, now); err == nil {
		t.Fatal("missing signer must fail closed")
	}
}

func TestEdgeJWKSHTTP(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(private)
	publicDER, _ := x509.MarshalPKIXPublicKey(&private.PublicKey)
	signer, err := newEdgeTokenSignerFromStrings(
		t.Context(),
		"",
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
	)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{EdgeTokens: signer}
	w := httptest.NewRecorder()
	s.edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("response: %d %v", w.Code, w.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	keys, ok := body["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("JWKS: %s", w.Body.String())
	}
	key := keys[0].(map[string]any)
	if key["kid"] != signer.JWKS(t.Context()).Keys[0].Kid || key["d"] != nil {
		t.Fatalf("JWKS exposed wrong key: %s", w.Body.String())
	}
	s.EdgeTokens, _ = newEdgeTokenSignerFromStrings(t.Context(), "", "", "")
	w = httptest.NewRecorder()
	s.edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"keys":[]}` || w.Header().Get(common.HeaderCacheControl) != "no-store" {
		t.Fatalf("disabled JWKS: %d %s %v", w.Code, w.Body.String(), w.Header())
	}
	s.EdgeTokens = nil
	w = httptest.NewRecorder()
	s.edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"keys":[]}` || w.Header().Get(common.HeaderCacheControl) != "no-store" {
		t.Fatalf("unconfigured JWKS: %d %s %v", w.Code, w.Body.String(), w.Header())
	}
}
