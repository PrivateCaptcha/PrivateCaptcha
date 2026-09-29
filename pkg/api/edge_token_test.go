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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
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

func TestEdgeTokenKeysReload(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenPublicKeyKey, firstPublic)
	signer := NewEdgeTokenSigner("", private, public)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	firstKid := signer.JWKS(t.Context()).Keys[0].Kid
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 2 || keys[0].Kid != firstKid || keys[1].Kid == firstKid {
		t.Fatalf("rotated key not staged alongside current key: %+v", keys)
	}
	public.value.Store(firstPublic)
	if err := signer.Update(t.Context()); err == nil {
		t.Fatal("mismatched reloaded keys accepted")
	}
	if len(signer.JWKS(t.Context()).Keys) != 0 {
		t.Fatal("invalid reload kept signing key active")
	}
}

func TestEdgeTokenRotationStagesPublicKey(t *testing.T) {
	firstPrivate, firstPublic := newTestEdgeKeyPair(t)
	secondPrivate, secondPublic := newTestEdgeKeyPair(t)
	private := newEdgeTestConfigItem(common.EdgeTokenPrivateKeyKey, firstPrivate)
	public := newEdgeTestConfigItem(common.EdgeTokenPublicKeyKey, firstPublic)
	signer := NewEdgeTokenSigner("", private, public)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := signer.JWKS(t.Context()).Keys[0]
	private.value.Store(secondPrivate)
	public.value.Store(secondPublic)
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	staged := signer.JWKS(t.Context()).Keys
	if len(staged) != 2 || staged[0].Kid != first.Kid || staged[1].Kid == first.Kid {
		t.Fatalf("rotation must publish both keys before switching: %+v", staged)
	}
	switchAt := signer.state.switchAt
	second := staged[1]
	w := httptest.NewRecorder()
	(&Server{EdgeTokens: signer}).edgeJWKSHandler(w, httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil))
	var published EdgeJWKS
	if err := json.Unmarshal(w.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if len(published.Keys) != 2 || published.Keys[0].Kid != first.Kid || published.Keys[1].Kid != second.Kid {
		t.Fatalf("JWKS endpoint did not prepublish replacement key: %+v", published.Keys)
	}
	token, err := signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), maxEdgeTokenTTL)
	if err != nil || !verifyTestEdgeSignature(first, token) || verifyTestEdgeSignature(second, token) {
		t.Fatalf("rotation switched signing key before JWKS caches expired: %v", err)
	}
	if err := signer.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !signer.state.switchAt.Equal(switchAt) {
		t.Fatal("reloading same key restarted staging")
	}
	token, err = signer.Sign(t.Context(), "sitekey", "example.com", time.Now(), maxEdgeTokenTTL)
	if err != nil || !verifyTestEdgeSignature(first, token) {
		t.Fatalf("reloading same key restarted or shortened staging: %v", err)
	}
	signer.mu.Lock()
	signer.state.switchAt = time.Now().Add(-time.Second)
	signer.mu.Unlock()
	activatedAfter := time.Now()
	token, err = signer.Sign(t.Context(), "sitekey", "example.com", activatedAfter, time.Hour)
	if err != nil || !verifyTestEdgeSignature(second, token) {
		t.Fatalf("rotation did not switch after JWKS cache window: %v", err)
	}
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 2 || keys[0].Kid != second.Kid || keys[1].Kid != first.Kid {
		t.Fatalf("rotation dropped previous verification key too early: %+v", keys)
	}
	if removeAt := signer.state.retired[0].removeAt; removeAt.Before(activatedAfter.Add(maxEdgeTokenTTL)) || removeAt.After(time.Now().Add(maxEdgeTokenTTL)) {
		t.Fatalf("previous key has incorrect retention deadline: %v", removeAt)
	}
	signer.mu.Lock()
	signer.state.retired[0].removeAt = time.Now().Add(-time.Second)
	signer.mu.Unlock()
	if keys := signer.JWKS(t.Context()).Keys; len(keys) != 1 || keys[0].Kid != second.Kid {
		t.Fatalf("expired previous key still published: %+v", keys)
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
	signer := NewEdgeTokenSigner("", cfg.Get(common.EdgeTokenPrivateKeyKey), cfg.Get(common.EdgeTokenPublicKeyKey))
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
		if err := signer.ValidateProperty(domain, time.Hour); err == nil {
			t.Errorf("accepted invalid property domain %q", domain)
		}
	}
	if err := signer.ValidateProperty("example.com", time.Hour); err != nil {
		t.Fatalf("rejected valid property domain: %v", err)
	}
	if err := signer.ValidateProperty("", 0); err != nil {
		t.Fatalf("rejected disabled edge with no property domain: %v", err)
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
		valid           bool
	}{
		{"absent", "", "", true},
		{"private only", privatePEM, "", false},
		{"public only", "", publicPEM, false},
		{"mismatch", privatePEM, otherPublicPEM, false},
		{"paired", privatePEM, publicPEM, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := newEdgeTokenSignerFromStrings(t.Context(), myAuthority, tt.priv, tt.pub)
			if (err == nil) != tt.valid {
				t.Fatalf("signer=%v, error=%v", signer, err)
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
	signer := NewEdgeTokenSigner(authority, newEdgeTestConfigItem(common.EdgeTokenPrivateKeyKey, private), newEdgeTestConfigItem(common.EdgeTokenPublicKeyKey, public))
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
	s := &Server{EdgeTokens: signer}
	now := time.Unix(1700000000, 0)
	result := &puzzle.VerifyResult{Error: puzzle.VerifyNoError, Sitekey: "first-sitekey", Domain: "example.com", EdgeTokenValidityInterval: time.Hour}
	response := newVerificationResponse(result, false)
	if err := s.addEdgeToken(t.Context(), response, result, now); err != nil {
		t.Fatal(err)
	}
	if response.EdgeToken == "" || !verifyTestEdgeSignature(signer.JWKS(t.Context()).Keys[0], response.EdgeToken) {
		t.Fatal("successful verification must return signed token")
	}
	result.Error = puzzle.VerifiedBeforeError
	response = newVerificationResponse(result, false)
	if err := s.addEdgeToken(t.Context(), response, result, now); err != nil || response.EdgeToken != "" {
		t.Fatalf("failed verification returned token: %v, %v", response.EdgeToken, err)
	}
	result.Error = puzzle.VerifyNoError
	result.EdgeTokenValidityInterval = 0
	response = newVerificationResponse(result, false)
	if err := s.addEdgeToken(t.Context(), response, result, now); err != nil || response.EdgeToken != "" {
		t.Fatalf("disabled setting returned token: %v, %v", response.EdgeToken, err)
	}
	s.EdgeTokens = nil
	result.EdgeTokenValidityInterval = time.Hour
	if err := s.addEdgeToken(t.Context(), response, result, now); err == nil {
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
	if w.Code != http.StatusOK || w.Body.String() != `{"keys":[]}` {
		t.Fatalf("disabled JWKS: %d %s", w.Code, w.Body.String())
	}
}
