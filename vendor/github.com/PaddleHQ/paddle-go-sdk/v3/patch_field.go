package paddle

import (
	"encoding/json"
)

// PatchField is a type that represents a field in a patch request.
// It is used to differentiate between:
// * a field that should not be present in the request.
// * a field that should be present in the request, but with a null value.
// * a field that should be present in the request, with a non-null value.
// Using the standard json package, it is not possible to differentiate between
// a field that is not present and a field that is present with an explicit
// null value without the use of maps.
// This type is used to work around this limitation, and provides some type
// safety by allowing the user to specify the type of the field as a
// type parameter.
// To create a new PatchField value, use the NewPatchField function.
type PatchField[T any] struct {
	value *T
}

// NewPatchField creates a new PatchField from a given value.
// This is used for fields that accept either a value, or omitted completely.
func NewPatchField[T any](value T) *PatchField[T] {
	return &PatchField[T]{value: &value}
}

// NewPtrPatchField creates a new PatchField from a concrete value, where the
// expected PatchField value is a pointer (aka optional).
// This is an alias to doing NewPatchField(ptrTo(v)), where ptrTo is a function
// that returns a pointer to the given value (e.g. &v).
func NewPtrPatchField[T any](value T) *PatchField[*T] {
	v := &value
	return &PatchField[*T]{value: &v}
}

// NewNullPatchField creates a new PatchField with a null value.
func NewNullPatchField[T any]() *PatchField[T] {
	return &PatchField[T]{value: nil}
}

// Value returns the value, if any.
func (f PatchField[T]) Value() *T {
	return f.value
}

// MarshalJSON implements the json.Marshaler interface.
// If the PatchField hasn't been set on the type, then the omitempty will handle
// it as an omitted field. If the PatchField has been set, then this will render
// the null value as a JSON null.
func (f PatchField[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}
