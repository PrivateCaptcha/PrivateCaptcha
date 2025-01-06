package paddle

import (
	"context"

	"github.com/PaddleHQ/paddle-go-sdk/v3/internal/client"
)

// ContextWithTransitID returns a new context with the provided transitID.
// This transit ID will then be present in requests to the Paddle API where this
// context is used. These can be used by Paddle support to aid in debugging your
// integration.
func ContextWithTransitID(ctx context.Context, transitID string) context.Context {
	return client.ContextWithTransitID(ctx, transitID)
}
