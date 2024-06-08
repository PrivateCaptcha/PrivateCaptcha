package paddle

// PtrTo creates a pointer from a given value.
func PtrTo[V any](v V) *V {
	return &v
}
