package common

import "context"

const (
	FeatureArgon2ID   = "argon2id"
	FeatureEdgeTokens = "edge_tokens"
)

// Nil IDs mean that the user or organization is absent.
type FeatureFlags interface {
	Enabled(ctx context.Context, feature string, userID, orgID *int32) bool
	Cleanup(ctx context.Context, userID, orgID *int32)
}

type EnabledFeatureFlags struct{}

func (EnabledFeatureFlags) Enabled(context.Context, string, *int32, *int32) bool {
	return true
}

func (EnabledFeatureFlags) Cleanup(ctx context.Context, userID, orgID *int32) {
	// BUMP
}
