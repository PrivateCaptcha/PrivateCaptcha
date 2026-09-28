package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/golang-jwt/jwt/v5"
)

const maxEdgeTokenTTL = 24 * time.Hour

type edgeJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type edgeJWKS struct {
	Keys []edgeJWK `json:"keys"`
}

type edgeKeyState struct {
	private *ecdsa.PrivateKey
	kid     string
	jwks    edgeJWKS
}

type EdgeTokenSigner struct {
	privateKey common.ConfigItem
	publicKey  common.ConfigItem
	state      atomic.Pointer[edgeKeyState]
}

func NewEdgeTokenSigner(privateKey, publicKey common.ConfigItem) (*EdgeTokenSigner, error) {
	signer := &EdgeTokenSigner{privateKey: privateKey, publicKey: publicKey}
	if err := signer.Update(); err != nil {
		return nil, err
	}
	return signer, nil
}

func (s *EdgeTokenSigner) Update() (err error) {
	if s == nil {
		return nil
	}
	defer func() {
		if err != nil {
			s.state.Store(nil)
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
		s.state.Store(nil)
		return nil
	}
	if privatePEM == "" || publicPEM == "" {
		return errors.New("edge token keys must be configured together")
	}
	private, err := jwt.ParseECPrivateKeyFromPEM([]byte(privatePEM))
	if err != nil {
		return fmt.Errorf("invalid edge token private key: %w", err)
	}
	public, err := jwt.ParseECPublicKeyFromPEM([]byte(publicPEM))
	if err != nil {
		return fmt.Errorf("invalid edge token public key: %w", err)
	}
	if public.Curve.Params().Name != "P-256" || private.Curve.Params().Name != "P-256" {
		return errors.New("edge token keys must be P-256")
	}
	privatePoint, err := private.PublicKey.Bytes()
	if err != nil {
		return err
	}
	point, err := public.Bytes()
	if err != nil || len(point) != 65 || !bytes.Equal(privatePoint, point) {
		return errors.New("edge token public key does not match private key")
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(der)
	encode := base64.RawURLEncoding.EncodeToString
	kid := encode(fingerprint[:])
	s.state.Store(&edgeKeyState{
		private: private,
		kid:     kid,
		jwks:    edgeJWKS{Keys: []edgeJWK{{Kty: "EC", Crv: "P-256", Alg: "ES256", Use: "sig", Kid: kid, X: encode(point[1:33]), Y: encode(point[33:])}}},
	})
	return nil
}

func (s *EdgeTokenSigner) JWKS() edgeJWKS {
	if s != nil {
		if state := s.state.Load(); state != nil {
			return state.jwks
		}
	}
	return edgeJWKS{Keys: []edgeJWK{}}
}

func (s *EdgeTokenSigner) ValidateProperty(domain string, ttl time.Duration) error {
	if err := s.ValidateLifetime(ttl); err != nil || ttl == 0 {
		return err
	}
	if domain == "" {
		return errors.New("edge token signing requires a property domain")
	}
	return nil
}

func (s *EdgeTokenSigner) ValidateLifetime(ttl time.Duration) error {
	if ttl == 0 {
		return nil
	}
	if ttl < time.Second || ttl > maxEdgeTokenTTL || s == nil || s.state.Load() == nil {
		return errors.New("edge token signing requires a valid lifetime and keys")
	}
	return nil
}

func (s *EdgeTokenSigner) Sign(sitekey, host string, now time.Time, ttl time.Duration) (string, error) {
	if sitekey == "" || s.ValidateProperty(host, ttl) != nil || ttl == 0 {
		return "", errors.New("invalid edge token request")
	}
	state := s.state.Load()
	if state == nil {
		return "", errors.New("edge token signing keys are unavailable")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"v": 1, "iss": "https://api.privatecaptcha.com", "aud": sitekey, "host": host,
		"iat": now.Unix(), "exp": now.Unix() + int64(ttl/time.Second),
	})
	token.Header["typ"] = "pc-edge+jwt"
	token.Header["kid"] = state.kid
	return token.SignedString(state.private)
}

func (s *Server) edgeJWKSHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(common.HeaderContentType, common.ContentTypeJSON)
	w.Header().Set(common.HeaderCacheControl, "public, max-age=300")
	_ = json.NewEncoder(w).Encode(s.EdgeTokens.JWKS())
}
