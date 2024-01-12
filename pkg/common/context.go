package common

type ContextKey int

const (
	IPAddressContextKey ContextKey = iota
	TraceIDContextKey   ContextKey = iota
	ClaimsContextKey    ContextKey = iota
	PropertyContextKey  ContextKey = iota
	APIKeyContextKey    ContextKey = iota
)
