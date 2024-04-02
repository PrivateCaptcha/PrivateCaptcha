package common

type ContextKey int

const (
	IPAddressContextKey      ContextKey = iota
	TraceIDContextKey        ContextKey = iota
	ClaimsContextKey         ContextKey = iota
	PropertyAndOrgContextKey ContextKey = iota
	APIKeyContextKey         ContextKey = iota
	LoggedInContextKey       ContextKey = iota
	OrgIDContextKey          ContextKey = iota
)
