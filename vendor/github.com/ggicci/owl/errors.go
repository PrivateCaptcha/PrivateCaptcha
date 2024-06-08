package owl

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupportedType      = errors.New("unsupported type")
	ErrInvalidDirectiveName = errors.New("invalid directive name")
	ErrDuplicateDirective   = errors.New("duplicate directive")
	ErrMissingExecutor      = errors.New("missing executor")
	ErrTypeMismatch         = errors.New("type mismatch")
	ErrScanNilField         = errors.New("scan nil field")
	ErrInvalidResolveTarget = errors.New("invalid resolve target")
)

func invalidDirectiveName(name string) error {
	return fmt.Errorf("%w: %q (should comply with %s)", ErrInvalidDirectiveName, name, reDirectiveName.String())
}

func duplicateDirective(name string) error {
	return fmt.Errorf("%w: %q (defined in the same struct tag)", ErrDuplicateDirective, name)
}

func duplicateExecutor(name string) error {
	return fmt.Errorf("duplicate executor: %q (registered to the same namespace)", name)
}

func nilExecutor(name string) error {
	return fmt.Errorf("nil executor: %q", name)
}

type ResolveError struct {
	fieldError
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("resolve field %q failed: %s", e.Resolver.String(), e.Err)
}

type ScanError struct {
	fieldError
}

func (e *ScanError) Error() string {
	return fmt.Sprintf("scan field %q failed: %s", e.Resolver.String(), e.Err)
}

type fieldError struct {
	Err      error
	Resolver *Resolver
}

func (e *fieldError) Unwrap() error {
	return e.Err
}

func (e *fieldError) AsDirectiveExecutionError() *DirectiveExecutionError {
	var de *DirectiveExecutionError
	errors.As(e.Err, &de)
	return de
}

type DirectiveExecutionError struct {
	Err error
	Directive
}

func (e *DirectiveExecutionError) Error() string {
	return fmt.Sprintf("execute directive %q with args %v failed: %s", e.Directive.Name, e.Directive.Argv, e.Err)
}

func (e *DirectiveExecutionError) Unwrap() error {
	return e.Err
}
