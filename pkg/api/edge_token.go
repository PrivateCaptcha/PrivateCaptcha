package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/idna"
)

const (
	maxEdgeTokenTTL      = 24 * time.Hour
	edgeJWKSCacheTTL     = 300 * time.Second
	edgeDefaultAuthority = "https://api.privatecaptcha.com"
	edgeKeyType          = "EC"
	edgeKeyCurve         = "P-256"
	edgeKeyAlgorithm     = "ES256"
	edgeKeyUse           = "sig"
	edgeTokenType        = "pc-edge+jwt"
)

var (
	errEdgeInvalidDomain        = errors.New("edge token signing requires a valid property domain")
	errEdgeInvalidLifetime      = errors.New("edge token signing requires a valid lifetime and keys")
	errEdgeInvalidRequest       = errors.New("invalid edge token request")
	errEdgeKeysUnavailable      = errors.New("edge token signing keys are unavailable")
	errEdgeKeysUnpaired         = errors.New("edge token keys must be configured together")
	errEdgePreviousKeysUnpaired = errors.New("previous edge token keys must be configured together")
	errEdgeInvalidPrivateKey    = errors.New("invalid edge token private key")
	errEdgeInvalidPublicKey     = errors.New("invalid edge token public key")
	errEdgeInvalidKeyCurve      = errors.New("edge token keys must be P-256")
	errEdgeKeyMismatch          = errors.New("edge token public key does not match private key")
	errEdgeSigningFailed        = errors.New("failed to sign edge token")
)

type edgeJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type edgeKeyState struct {
	active   edgeSigningKey
	pending  *edgeSigningKey
	switchAt time.Time
	previous *edgeJWK
}

type edgeSigningKey struct {
	private *ecdsa.PrivateKey
	public  edgeJWK
}

type EdgeTokenSigner struct {
	authority          string
	privateKey         common.ConfigItem
	publicKey          common.ConfigItem
	previousPrivateKey common.ConfigItem
	previousPublicKey  common.ConfigItem
	mu                 sync.Mutex
	state              *edgeKeyState
}

func NewEdgeTokenSigner(authority string, cfg common.ConfigStore) *EdgeTokenSigner {
	if len(authority) == 0 {
		authority = edgeDefaultAuthority
	}
	return &EdgeTokenSigner{
		authority:          authority,
		privateKey:         cfg.Get(common.EdgeTokenPrivateKeyKey),
		publicKey:          cfg.Get(common.EdgeTokenPublicKeyKey),
		previousPrivateKey: cfg.Get(common.EdgeTokenPreviousPrivateKeyKey),
		previousPublicKey:  cfg.Get(common.EdgeTokenPreviousPublicKeyKey),
	}
}

func (s *EdgeTokenSigner) Update(ctx context.Context) error {
	if s == nil {
		return nil
	}
	privatePEM, publicPEM := s.privateKey.Value(), s.publicKey.Value()
	previousPrivatePEM, previousPublicPEM := s.previousPrivateKey.Value(), s.previousPublicKey.Value()
	if privatePEM == "" && publicPEM == "" && previousPrivatePEM == "" && previousPublicPEM == "" {
		s.mu.Lock()
		s.state = nil
		s.mu.Unlock()
		slog.DebugContext(ctx, "Edge token signing keys are disabled")
		return nil
	}
	if privatePEM == "" || publicPEM == "" {
		slog.DebugContext(ctx, "Edge token key pair is incomplete", "privateKeyConfigured", privatePEM != "", "publicKeyConfigured", publicPEM != "")
		return errEdgeKeysUnpaired
	}
	key, err := parseEdgeSigningKey(ctx, privatePEM, publicPEM)
	if err != nil {
		slog.DebugContext(ctx, "Failed to parse current edge token keys", common.ErrAttr(err))
		return err
	}
	var previous *edgeSigningKey
	if previousPrivatePEM != "" || previousPublicPEM != "" {
		if previousPrivatePEM == "" || previousPublicPEM == "" {
			slog.DebugContext(ctx, "Previous edge token key pair is incomplete", "privateKeyConfigured", previousPrivatePEM != "", "publicKeyConfigured", previousPublicPEM != "")
			return errEdgePreviousKeysUnpaired
		}
		oldKey, err := parseEdgeSigningKey(ctx, previousPrivatePEM, previousPublicPEM)
		if err != nil {
			slog.DebugContext(ctx, "Failed to parse previous edge token keys", common.ErrAttr(err))
			return err
		}
		if oldKey.public.Kid != key.public.Kid {
			previous = &oldKey
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.advanceLocked(ctx, now)
	state := s.state
	if state == nil || (state.active.public.Kid != key.public.Kid && (previous == nil || state.active.public.Kid != previous.public.Kid)) {
		state = &edgeKeyState{active: key}
		if previous != nil {
			state.active = *previous
		}
		s.state = state
	}
	state.previous = nil
	if previous != nil {
		state.previous = &previous.public
	}
	if state.active.public.Kid == key.public.Kid {
		state.pending = nil
	} else if state.pending == nil || state.pending.public.Kid != key.public.Kid {
		state.pending = &key
		state.switchAt = now.Add(edgeJWKSCacheTTL)
		slog.InfoContext(ctx, "Staging edge signing key rotation", "currentKid", state.active.public.Kid, "nextKid", key.public.Kid, "switchAt", state.switchAt)
	}
	return nil
}

func parseEdgeSigningKey(ctx context.Context, privatePEM, publicPEM string) (edgeSigningKey, error) {
	private, err := jwt.ParseECPrivateKeyFromPEM([]byte(privatePEM))
	if err != nil {
		slog.DebugContext(ctx, "Failed to parse edge token private key", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPrivateKey
	}
	public, err := jwt.ParseECPublicKeyFromPEM([]byte(publicPEM))
	if err != nil {
		slog.DebugContext(ctx, "Failed to parse edge token public key", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPublicKey
	}
	if public.Curve.Params().Name != edgeKeyCurve || private.Curve.Params().Name != edgeKeyCurve {
		slog.DebugContext(ctx, "Unsupported edge token key curve", "privateCurve", private.Curve.Params().Name, "publicCurve", public.Curve.Params().Name)
		return edgeSigningKey{}, errEdgeInvalidKeyCurve
	}
	privatePoint, err := private.PublicKey.Bytes()
	if err != nil {
		slog.DebugContext(ctx, "Failed to encode edge token private key public point", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPrivateKey
	}
	point, err := public.Bytes()
	if err != nil {
		slog.DebugContext(ctx, "Failed to encode edge token public point", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPublicKey
	}
	if len(point) != 65 || !bytes.Equal(privatePoint, point) {
		slog.DebugContext(ctx, "Edge token key pair does not match", "publicPointLength", len(point))
		return edgeSigningKey{}, errEdgeKeyMismatch
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		slog.DebugContext(ctx, "Failed to marshal edge token public key", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPublicKey
	}
	fingerprint := sha256.Sum256(der)
	encode := base64.RawURLEncoding.EncodeToString
	return edgeSigningKey{private: private, public: edgeJWK{
		Kty: edgeKeyType,
		Crv: edgeKeyCurve,
		Alg: edgeKeyAlgorithm,
		Use: edgeKeyUse,
		Kid: encode(fingerprint[:]),
		X:   encode(point[1:33]),
		Y:   encode(point[33:]),
	}}, nil
}

// advanceLocked replaces the current signing key (active) with the next key (pending)
// when switchAt is reached. Update advertises pending in JWKS one cache TTL beforehand,
// so verifiers refresh their cached keys before we start signing with the new key.
// The configured previous public key stays in JWKS to verify already-issued tokens.
// Caller must hold s.mu.
func (s *EdgeTokenSigner) advanceLocked(ctx context.Context, now time.Time) {
	state := s.state
	if state == nil {
		return
	}
	if state.pending != nil && !now.Before(state.switchAt) {
		previous := state.active.public
		state.active = *state.pending
		state.pending = nil
		slog.InfoContext(ctx, "Activated edge signing key", "kid", state.active.public.Kid, "previousKid", previous.Kid)
	}
}

func (s *EdgeTokenSigner) JWKS(ctx context.Context) EdgeJWKS {
	if s == nil {
		return EdgeJWKS{Keys: []edgeJWK{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(ctx, time.Now())
	if s.state == nil {
		return EdgeJWKS{Keys: []edgeJWK{}}
	}
	keys := make([]edgeJWK, 0, 3)
	addKey := func(key edgeJWK) {
		for _, present := range keys {
			if present.Kid == key.Kid {
				return
			}
		}
		keys = append(keys, key)
	}
	addKey(s.state.active.public)
	if s.state.pending != nil {
		addKey(s.state.pending.public)
	}
	if s.state.previous != nil {
		addKey(*s.state.previous)
	}
	return EdgeJWKS{Keys: keys}
}

func (s *EdgeTokenSigner) ValidateProperty(ctx context.Context, domain string, ttl time.Duration) error {
	if err := s.ValidateLifetime(ctx, ttl); err != nil || ttl == 0 {
		return err
	}
	if domain == "" || common.IsLocalhost(domain) || common.IsIPAddress(domain) {
		slog.DebugContext(ctx, "Invalid property domain for edge tokens", "domain", domain)
		return errEdgeInvalidDomain
	}
	if _, err := idna.Lookup.ToASCII(domain); err != nil {
		slog.DebugContext(ctx, "Failed to normalize property domain for edge tokens", "domain", domain, common.ErrAttr(err))
		return errEdgeInvalidDomain
	}
	return nil
}

func (s *EdgeTokenSigner) ValidateLifetime(ctx context.Context, ttl time.Duration) error {
	if ttl == 0 {
		return nil
	}
	if ttl < time.Second || ttl > maxEdgeTokenTTL || s == nil {
		slog.DebugContext(ctx, "Invalid edge token lifetime or missing signer", "ttl", ttl, "signerConfigured", s != nil)
		return errEdgeInvalidLifetime
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		slog.DebugContext(ctx, "Edge token signing keys are unavailable", "ttl", ttl)
		return errEdgeInvalidLifetime
	}
	return nil
}

func (s *EdgeTokenSigner) Sign(ctx context.Context, sitekey, host string, now time.Time, ttl time.Duration) (string, error) {
	if host == "" || sitekey == "" || s == nil || ttl == 0 {
		slog.DebugContext(ctx, "Invalid edge token signing request", "host", host, "sitekey", sitekey, "ttl", ttl, "signerConfigured", s != nil)
		return "", errEdgeInvalidRequest
	}
	s.mu.Lock()
	// now sets the JWT's iat/exp and may be stale by the time we acquire this lock.
	// Key activation must wait a full JWKS cache TTL on this server, independently
	// of that timestamp. A fresh time.Now() preserves the monotonic clock used by
	// switchAt; the caller's UTC conversion strips it, making the wait vulnerable
	// to wall-clock jumps that could activate the new key before caches expire.
	s.advanceLocked(ctx, time.Now())
	var key edgeSigningKey
	if s.state != nil {
		key = s.state.active
	}
	s.mu.Unlock()
	if key.private == nil {
		slog.DebugContext(ctx, "Edge token signing keys are unavailable", "sitekey", sitekey)
		return "", errEdgeKeysUnavailable
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256,
		jwt.MapClaims{
			"v":    1,
			"iss":  s.authority,
			"aud":  sitekey,
			"host": host,
			"iat":  now.Unix(),
			"exp":  now.Unix() + int64(ttl/time.Second),
		})
	token.Header["typ"] = edgeTokenType
	token.Header["kid"] = key.public.Kid
	signed, err := token.SignedString(key.private)
	if err != nil {
		slog.DebugContext(ctx, "Failed to sign edge token", "kid", key.public.Kid, "sitekey", sitekey, common.ErrAttr(err))
		return "", errEdgeSigningFailed
	}
	return signed, nil
}

func (s *Server) edgeJWKSHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set(common.HeaderCacheControl, "public, max-age="+strconv.Itoa(int(edgeJWKSCacheTTL/time.Second)))
	common.SendJSONResponse(ctx, w, s.EdgeTokens.JWKS(ctx))
}
