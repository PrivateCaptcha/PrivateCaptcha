package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
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
	errEdgeInvalidDomain   = errors.New("edge token signing requires a valid property domain")
	errEdgeInvalidLifetime = errors.New("edge token signing requires a valid lifetime and keys")
	errEdgeInvalidRequest  = errors.New("invalid edge token request")
	errEdgeKeysUnavailable = errors.New("edge token signing keys are unavailable")
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
	retired  []edgeRetiredKey
}

type edgeSigningKey struct {
	private *ecdsa.PrivateKey
	public  edgeJWK
}

type edgeRetiredKey struct {
	public   edgeJWK
	removeAt time.Time
}

type EdgeTokenSigner struct {
	authority  string
	privateKey common.ConfigItem
	publicKey  common.ConfigItem
	mu         sync.Mutex
	state      *edgeKeyState
}

func NewEdgeTokenSigner(authority string, privateKey, publicKey common.ConfigItem) *EdgeTokenSigner {
	if len(authority) == 0 {
		authority = edgeDefaultAuthority
	}
	return &EdgeTokenSigner{authority: authority, privateKey: privateKey, publicKey: publicKey}
}

func (s *EdgeTokenSigner) Update(ctx context.Context) (err error) {
	if s == nil {
		return nil
	}
	defer func() {
		if err != nil {
			s.mu.Lock()
			s.state = nil
			s.mu.Unlock()
		}
	}()
	privatePEM, publicPEM := "", ""
	if s.privateKey != nil {
		privatePEM = s.privateKey.Value()
	}
	if s.publicKey != nil {
		publicPEM = s.publicKey.Value()
	}
	if privatePEM == "" && publicPEM == "" {
		s.mu.Lock()
		s.state = nil
		s.mu.Unlock()
		return nil
	}
	if privatePEM == "" || publicPEM == "" {
		return errors.New("edge token keys must be configured together")
	}
	key, err := parseEdgeSigningKey(privatePEM, publicPEM)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.advanceLocked(ctx, now)
	state := s.state
	if state == nil {
		s.state = &edgeKeyState{active: key}
	} else if state.active.public.Kid == key.public.Kid {
		state.pending = nil
	} else if state.pending == nil || state.pending.public.Kid != key.public.Kid {
		state.pending = &key
		state.switchAt = now.Add(edgeJWKSCacheTTL)
		slog.InfoContext(ctx, "Staging edge signing key rotation", "currentKid", state.active.public.Kid, "nextKid", key.public.Kid, "switchAt", state.switchAt)
	}
	return nil
}

func parseEdgeSigningKey(privatePEM, publicPEM string) (edgeSigningKey, error) {
	private, err := jwt.ParseECPrivateKeyFromPEM([]byte(privatePEM))
	if err != nil {
		return edgeSigningKey{}, fmt.Errorf("invalid edge token private key: %w", err)
	}
	public, err := jwt.ParseECPublicKeyFromPEM([]byte(publicPEM))
	if err != nil {
		return edgeSigningKey{}, fmt.Errorf("invalid edge token public key: %w", err)
	}
	if public.Curve.Params().Name != edgeKeyCurve || private.Curve.Params().Name != edgeKeyCurve {
		return edgeSigningKey{}, errors.New("edge token keys must be P-256")
	}
	privatePoint, err := private.PublicKey.Bytes()
	if err != nil {
		return edgeSigningKey{}, err
	}
	point, err := public.Bytes()
	if err != nil || len(point) != 65 || !bytes.Equal(privatePoint, point) {
		return edgeSigningKey{}, errors.New("edge token public key does not match private key")
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return edgeSigningKey{}, err
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

func (s *EdgeTokenSigner) advanceLocked(ctx context.Context, now time.Time) {
	state := s.state
	if state == nil {
		return
	}
	if state.pending != nil && !now.Before(state.switchAt) {
		previous := state.active.public
		state.retired = append(state.retired, edgeRetiredKey{public: previous, removeAt: now.Add(maxEdgeTokenTTL)})
		state.active = *state.pending
		state.pending = nil
		slog.InfoContext(ctx, "Activated edge signing key", "kid", state.active.public.Kid, "previousKid", previous.Kid)
	}
	remaining := state.retired[:0]
	for _, key := range state.retired {
		if now.Before(key.removeAt) {
			remaining = append(remaining, key)
		}
	}
	state.retired = remaining
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
	keys := make([]edgeJWK, 0, 2+len(s.state.retired))
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
	for _, key := range s.state.retired {
		addKey(key.public)
	}
	return EdgeJWKS{Keys: keys}
}

func (s *EdgeTokenSigner) ValidateProperty(domain string, ttl time.Duration) error {
	if err := s.ValidateLifetime(ttl); err != nil || ttl == 0 {
		return err
	}
	if domain == "" || common.IsLocalhost(domain) || common.IsIPAddress(domain) {
		return errEdgeInvalidDomain
	}
	if _, err := idna.Lookup.ToASCII(domain); err != nil {
		return errEdgeInvalidDomain
	}
	return nil
}

func (s *EdgeTokenSigner) ValidateLifetime(ttl time.Duration) error {
	if ttl == 0 {
		return nil
	}
	if ttl < time.Second || ttl > maxEdgeTokenTTL || s == nil {
		return errEdgeInvalidLifetime
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		return errEdgeInvalidLifetime
	}
	return nil
}

func (s *EdgeTokenSigner) Sign(ctx context.Context, sitekey, host string, now time.Time, ttl time.Duration) (string, error) {
	if sitekey == "" || s.ValidateLifetime(ttl) != nil || ttl == 0 {
		return "", errEdgeInvalidRequest
	}
	s.mu.Lock()
	s.advanceLocked(ctx, time.Now())
	var key edgeSigningKey
	if s.state != nil {
		key = s.state.active
	}
	s.mu.Unlock()
	if key.private == nil {
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
	return token.SignedString(key.private)
}

func (s *Server) edgeJWKSHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set(common.HeaderCacheControl, "public, max-age="+strconv.Itoa(int(edgeJWKSCacheTTL/time.Second)))
	common.SendJSONResponse(ctx, w, s.EdgeTokens.JWKS(ctx))
}
