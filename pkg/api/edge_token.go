//go:build enterprise

package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"slices"
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
	errEdgeInvalidDomain     = errors.New("edge token signing requires a valid property domain")
	errEdgeInvalidLifetime   = errors.New("edge token signing requires a valid lifetime and keys")
	errEdgeInvalidRequest    = errors.New("invalid edge token request")
	errEdgeKeysUnavailable   = errors.New("edge token signing keys are unavailable")
	errEdgeKeysUnpaired      = errors.New("edge token signing keys must be configured together")
	errEdgeInvalidPrivateKey = errors.New("invalid edge token private key")
	errEdgeInvalidPublicKey  = errors.New("invalid edge token public key")
	errEdgeInvalidKeyCurve   = errors.New("edge token keys must be P-256")
	errEdgeKeyMismatch       = errors.New("edge token public key does not match private key")
	errEdgeSigningFailed     = errors.New("failed to sign edge token")
)

type edgeKeyState struct {
	signing   edgeSigningKey
	secondary *edgeJWK
	retired   []edgeVerificationKey
}

type edgeSigningKey struct {
	private           *ecdsa.PrivateKey
	public            edgeJWK
	latestTokenExpiry time.Time
}

type edgeVerificationKey struct {
	public    edgeJWK
	expiresAt time.Time
}

// EdgeTokenSigner always signs with PC_EDGE_TOKEN_SIGNING_PRIVATE_KEY and publishes
// its public key together with the optional PC_EDGE_TOKEN_SECONDARY_PUBLIC_KEY.
// The secondary key is verification-only; it can be upcoming or retiring.
//
// Rotation requires an explicit two-phase rollout:
//  1. Publish: keep the old signing pair and configure the new public key as
//     secondary on every replica. Wait until all old-only JWKS replicas and
//     responses are drained, then wait edgeJWKSCacheTTL plus a safety margin.
//  2. Activate: deploy the new signing pair with the old public key as secondary.
//     Both phases publish both keys, so activation can roll across replicas.
//  3. Retain: keep the old public key as secondary on every replica for at least
//     maxEdgeTokenTTL after the last old-key token is issued, plus verifier
//     clock/leeway margins. Only then clear or reuse the secondary key slot.
//
// Apply the signing pair and secondary key together as one configuration change.
// Process-local retired keys protect live reloads but do not survive restarts;
// they do not replace the cluster-wide publish, wait, and retention steps.
type EdgeTokenSigner struct {
	authority          string
	signingPrivateKey  common.ConfigItem
	signingPublicKey   common.ConfigItem
	secondaryPublicKey common.ConfigItem
	mu                 sync.Mutex
	state              *edgeKeyState
}

func NewEdgeTokenSigner(authority string, cfg common.ConfigStore) *EdgeTokenSigner {
	if len(authority) == 0 {
		authority = edgeDefaultAuthority
	}
	return &EdgeTokenSigner{
		authority:          authority,
		signingPrivateKey:  cfg.Get(common.EdgeTokenSigningPrivateKeyKey),
		signingPublicKey:   cfg.Get(common.EdgeTokenSigningPublicKeyKey),
		secondaryPublicKey: cfg.Get(common.EdgeTokenSecondaryPublicKeyKey),
	}
}

func (s *EdgeTokenSigner) Update(ctx context.Context) error {
	if s == nil {
		return nil
	}
	privatePEM, publicPEM := s.signingPrivateKey.Value(), s.signingPublicKey.Value()
	secondaryPEM := s.secondaryPublicKey.Value()
	if privatePEM == "" && publicPEM == "" && secondaryPEM == "" {
		s.mu.Lock()
		s.state = nil
		s.mu.Unlock()
		slog.WarnContext(ctx, "Edge token signing keys are disabled")
		return nil
	}
	if privatePEM == "" || publicPEM == "" {
		slog.DebugContext(ctx, "Edge token key pair is incomplete", "privateKeyConfigured", privatePEM != "", "publicKeyConfigured", publicPEM != "")
		return errEdgeKeysUnpaired
	}
	key, err := parseEdgeSigningKey(ctx, privatePEM, publicPEM)
	if err != nil {
		slog.DebugContext(ctx, "Failed to parse edge token signing keys", common.ErrAttr(err))
		return err
	}
	var secondary *edgeJWK
	if secondaryPEM != "" {
		public, err := parseEdgePublicKey(ctx, secondaryPEM)
		if err != nil {
			slog.DebugContext(ctx, "Failed to parse secondary edge token public key", common.ErrAttr(err))
			return err
		}
		if public.Kid != key.public.Kid {
			secondary = &public
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneRetiredLocked(now)
	state := s.state
	if state == nil {
		state = &edgeKeyState{
			signing: key,
		}
		s.state = state
	} else if state.signing.public.Kid != key.public.Kid {
		expiresAt := now.Add(maxEdgeTokenTTL).UTC()
		if state.signing.latestTokenExpiry.After(expiresAt) {
			expiresAt = state.signing.latestTokenExpiry
		}
		state.retired = append(state.retired, edgeVerificationKey{
			public:    state.signing.public,
			expiresAt: expiresAt,
		})
		slog.InfoContext(ctx, "Activated edge signing key", "kid", key.public.Kid, "retiredKid", state.signing.public.Kid)
		state.signing = key
	}
	state.secondary = secondary
	return nil
}

func parseEdgeSigningKey(ctx context.Context, privatePEM, publicPEM string) (edgeSigningKey, error) {
	private, err := jwt.ParseECPrivateKeyFromPEM([]byte(privatePEM))
	if err != nil {
		slog.DebugContext(ctx, "Failed to parse edge token private key", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPrivateKey
	}
	public, err := parseEdgePublicKey(ctx, publicPEM)
	if err != nil {
		return edgeSigningKey{}, err
	}
	if private.Curve.Params().Name != edgeKeyCurve {
		slog.DebugContext(ctx, "Unsupported edge token private key curve", "curve", private.Curve.Params().Name)
		return edgeSigningKey{}, errEdgeInvalidKeyCurve
	}
	point, err := private.PublicKey.Bytes()
	if err != nil {
		slog.DebugContext(ctx, "Failed to encode edge token private key public point", common.ErrAttr(err))
		return edgeSigningKey{}, errEdgeInvalidPrivateKey
	}
	encode := base64.RawURLEncoding.EncodeToString
	if len(point) != 65 || public.X != encode(point[1:33]) || public.Y != encode(point[33:]) {
		slog.DebugContext(ctx, "Edge token key pair does not match", "privatePointLength", len(point))
		return edgeSigningKey{}, errEdgeKeyMismatch
	}
	return edgeSigningKey{
		private: private,
		public:  public,
	}, nil
}

func parseEdgePublicKey(ctx context.Context, publicPEM string) (edgeJWK, error) {
	public, err := jwt.ParseECPublicKeyFromPEM([]byte(publicPEM))
	if err != nil {
		slog.DebugContext(ctx, "Failed to parse edge token public key", common.ErrAttr(err))
		return edgeJWK{}, errEdgeInvalidPublicKey
	}
	if public.Curve.Params().Name != edgeKeyCurve {
		slog.DebugContext(ctx, "Unsupported edge token public key curve", "curve", public.Curve.Params().Name)
		return edgeJWK{}, errEdgeInvalidKeyCurve
	}
	point, err := public.Bytes()
	if err != nil {
		slog.DebugContext(ctx, "Failed to encode edge token public point", common.ErrAttr(err))
		return edgeJWK{}, errEdgeInvalidPublicKey
	}
	if len(point) != 65 {
		slog.DebugContext(ctx, "Invalid edge token public point length", "length", len(point))
		return edgeJWK{}, errEdgeInvalidPublicKey
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		slog.DebugContext(ctx, "Failed to marshal edge token public key", common.ErrAttr(err))
		return edgeJWK{}, errEdgeInvalidPublicKey
	}
	fingerprint := sha256.Sum256(der)
	encode := base64.RawURLEncoding.EncodeToString
	return edgeJWK{
		Kty: edgeKeyType,
		Crv: edgeKeyCurve,
		Alg: edgeKeyAlgorithm,
		Use: edgeKeyUse,
		Kid: encode(fingerprint[:]),
		X:   encode(point[1:33]),
		Y:   encode(point[33:]),
	}, nil
}

// Caller must hold s.mu.
func (s *EdgeTokenSigner) pruneRetiredLocked(now time.Time) {
	state := s.state
	if state == nil {
		return
	}
	state.retired = slices.DeleteFunc(state.retired, func(key edgeVerificationKey) bool {
		return !now.Before(key.expiresAt)
	})
}

func (s *EdgeTokenSigner) JWKS(ctx context.Context) EdgeJWKS {
	if s == nil {
		return EdgeJWKS{Keys: []edgeJWK{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRetiredLocked(time.Now())
	if s.state == nil {
		return EdgeJWKS{Keys: []edgeJWK{}}
	}
	keys := make([]edgeJWK, 0, 2+len(s.state.retired))
	addKey := func(key edgeJWK) {
		for _, present := range keys {
			if present.Kid == key.Kid {
				return
			}
		}
		keys = append(keys, key)
	}
	addKey(s.state.signing.public)
	if s.state.secondary != nil {
		addKey(*s.state.secondary)
	}
	for _, key := range s.state.retired {
		addKey(key.public)
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
	expiresAt := time.Unix(now.Unix()+int64(ttl/time.Second), 0).UTC()
	s.mu.Lock()
	var key edgeSigningKey
	if s.state != nil {
		// Record expiry before unlocking so a concurrent rotation cannot retire this key too early.
		if expiresAt.After(s.state.signing.latestTokenExpiry) {
			s.state.signing.latestTokenExpiry = expiresAt
		}
		key = s.state.signing
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
			"exp":  expiresAt.Unix(),
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
	jwks := s.EdgeTokens.JWKS(ctx)
	if len(jwks.Keys) == 0 {
		w.Header().Set(common.HeaderCacheControl, "no-store")
	} else {
		w.Header().Set(common.HeaderCacheControl, "public, max-age="+strconv.Itoa(int(edgeJWKSCacheTTL/time.Second)))
	}
	common.SendJSONResponse(ctx, w, jwks)
}
