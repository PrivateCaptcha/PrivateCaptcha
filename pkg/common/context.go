package common

type ContextKey int

const (
	TraceIDContextKey ContextKey = iota
	PropertyContextKey
	APIKeyContextKey
	LoggedInContextKey
	SessionContextKey
	SitekeyContextKey
	SecretContextKey
	RateLimitKeyContextKey
	SessionIDContextKey
	ServiceContextKey
	TimeContextKey
	FormContextKey
	FormIDContextKey
	PathPatternContextKey
	// Add new fields _above_
	CONTEXT_KEYS_COUNT
)
