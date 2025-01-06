package client

import (
	"context"
)

// transitIDContextKey is the context key for the transit ID.
type transitIDContextKey struct{}

// ContextWithTransitID returns a new context with the provided transit ID.
func ContextWithTransitID(ctx context.Context, transitID string) context.Context {
	return context.WithValue(ctx, transitIDContextKey{}, transitID)
}

// TransitIDFromContext returns the transit ID from the provided context.
func TransitIDFromContext(ctx context.Context) string {
	transitID, _ := ctx.Value(transitIDContextKey{}).(string)

	return transitID
}
