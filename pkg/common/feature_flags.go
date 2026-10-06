package common

import "context"

const FeatureArgon2ID = "argon2id"

// Nil IDs mean that the user or organization is absent.
type FeatureFlags interface {
	Enabled(ctx context.Context, feature string, userID, orgID *int32) bool
}

type EnabledFeatureFlags struct{}

func (EnabledFeatureFlags) Enabled(context.Context, string, *int32, *int32) bool {
	return true
}
