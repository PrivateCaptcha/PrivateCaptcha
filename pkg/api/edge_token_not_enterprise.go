//go:build !enterprise

package api

import (
	"context"
	"errors"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type EdgeTokenSigner struct{}

func NewEdgeTokenSigner(string, common.ConfigStore) *EdgeTokenSigner {
	return &EdgeTokenSigner{}
}

func (*EdgeTokenSigner) Update(context.Context) error { return nil }

func (*EdgeTokenSigner) JWKS(context.Context) EdgeJWKS {
	return EdgeJWKS{Keys: []edgeJWK{}}
}

func (s *EdgeTokenSigner) ValidateProperty(ctx context.Context, _ string, ttl time.Duration) error {
	return s.ValidateLifetime(ctx, ttl)
}

func (*EdgeTokenSigner) ValidateLifetime(_ context.Context, ttl time.Duration) error {
	if ttl != 0 {
		return errors.New("edge protection requires an enterprise build")
	}
	return nil
}

func (*EdgeTokenSigner) Sign(context.Context, string, string, time.Time, time.Duration) (string, error) {
	return "", nil
}
