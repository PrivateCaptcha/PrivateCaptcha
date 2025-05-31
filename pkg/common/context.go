package common

type ContextKey int

const (
	TraceIDContextKey      ContextKey = iota
	PropertyContextKey     ContextKey = iota
	APIKeyContextKey       ContextKey = iota
	LoggedInContextKey     ContextKey = iota
	SessionContextKey      ContextKey = iota
	SitekeyContextKey      ContextKey = iota
	RateLimitKeyContextKey ContextKey = iota
	SessionIDContextKey    ContextKey = iota
	TimeContextKey         ContextKey = iota
)
