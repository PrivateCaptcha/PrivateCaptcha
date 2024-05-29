package common

type ContextKey int

const (
	IPAddressContextKey  ContextKey = iota
	TraceIDContextKey    ContextKey = iota
	ClaimsContextKey     ContextKey = iota
	PropertyContextKey   ContextKey = iota
	APIKeyContextKey     ContextKey = iota
	LoggedInContextKey   ContextKey = iota
	OrgIDContextKey      ContextKey = iota
	PropertyIDContextKey ContextKey = iota
	PeriodContextKey     ContextKey = iota
	KeyIDContextKey      ContextKey = iota
	UserIDContextKey     ContextKey = iota
	UserContextKey       ContextKey = iota
	SessionContextKey    ContextKey = iota
	SitekeyContextKey    ContextKey = iota
)
