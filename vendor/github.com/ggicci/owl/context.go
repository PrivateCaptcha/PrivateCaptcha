package owl

type contextKey int

const (
	ckNamespace contextKey = iota
	ckResolveNestedDirectives
)
