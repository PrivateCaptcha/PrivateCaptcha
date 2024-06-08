package owl

import (
	"context"
	"reflect"
	"regexp"
	"strings"
)

var reDirectiveName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// Directive defines the profile to locate a `DirectiveExecutor` instance
// and drives it with essential arguments.
type Directive struct {
	Name string   // name of the executor
	Argv []string // argv
}

// NewDirective creates a Directive instance.
func NewDirective(name string, argv ...string) *Directive {
	return &Directive{
		Name: name,
		Argv: argv,
	}
}

// ParseDirective creates a Directive instance by parsing a directive string
// extracted from the struct tag.
//
// Example directives are:
//
//	"form=page,page_index" -> { Name: "form", Args: ["page", "page_index"] }
//	"header=x-api-token"   -> { Name: "header", Args: ["x-api-token"] }
func ParseDirective(directive string) (*Directive, error) {
	directive = strings.TrimSpace(directive)
	parts := strings.SplitN(directive, "=", 2)
	name := parts[0]
	var argv []string
	if len(parts) == 2 {
		// Split the remained string by delimiter `,` as argv.
		// NOTE: the whiltespaces are kept here.
		// e.g. "query=page, index" -> { Name: "query", Args: ["page", " index"] }
		argv = strings.Split(parts[1], ",")
	}

	if !isValidDirectiveName(name) {
		return nil, invalidDirectiveName(name)
	}

	return NewDirective(name, argv...), nil
}

// Copy creates a copy of the directive. The copy is a deep copy.
func (d *Directive) Copy() *Directive {
	return NewDirective(d.Name, d.Argv...)
}

// String returns the string representation of the directive.
func (d *Directive) String() string {
	if len(d.Argv) == 0 {
		return d.Name
	}
	return d.Name + "=" + strings.Join(d.Argv, ",")
}

// DirectiveExecutor is the interface that wraps the Execute method.
// Execute executes the directive by passing the runtime context.
type DirectiveExecutor interface {
	Execute(*DirectiveRuntime) error
}

// DirecrtiveExecutorFunc is an adapter to allow the use of ordinary functions
// as DirectiveExecutors.
type DirectiveExecutorFunc func(*DirectiveRuntime) error

func (f DirectiveExecutorFunc) Execute(de *DirectiveRuntime) error {
	return f(de)
}

// DirectiveRuntime is the execution runtime/context of a directive. NOTE: the
// Directive and Resolver are both exported for the convenience but in an unsafe
// way. The user should not modify them. If you want to modify them, please call
// Resolver.Iterate to iterate the resolvers and modify them in the callback.
// And make sure this be done before any callings to Resolver.Resolve.
type DirectiveRuntime struct {
	Directive *Directive
	Resolver  *Resolver

	// Value is the reflect.Value of the field that the directive is applied to.
	// Worth noting that the value is a pointer to the field value. Which means
	// if the field is of type int. Then Value is of type *int. And the actual
	// value is stored in Value.Elem().Int(). The same for other types. So if you
	// want to modify the field value, you should call Value.Elem().Set(value),
	// e.g. Value.Elem().SetString(value), Value.Elem().SetInt(value), etc.
	Value reflect.Value

	// Context is the runtime context of the directive execution. The initial
	// context can be tweaked by applying options to Resolver.Resolve method.
	// Use WithValue option to set a value to the initial context. Each field
	// resolver will creates a new context by copying this initial context. And
	// for the directives of the same field resolver, they will use the same
	// context. Latter directives can use the values set by the former
	// directives. Ex:
	//
	//  type User struct {
	//      Name 	string `owl:"dirA;dirB"` // Context_1
	//      Gender 	string `owl:"dirC"`      // Context_2
	//  }
	//
	//  New(User{}).Resolve(WithValue("color", "red")) // Context_0
	//
	// In the above example, the initial context is Context_0. It has a value of "color" set to "red".
	// The context of the first field resolver is Context_1. It is created by copying Context_0.
	// The context of the second field resolver is Context_2. It is also created by copying Context_0.
	// Thus, all the directives during execution can access the value of "color" in Context_0.
	// For the Name field resolver, it has two directives, dirA and dirB. They will use the same context Context_1.
	// and if in dirA we set a value of "foo" to "bar", then in dirB we can get the value of "foo" as "bar".
	Context context.Context
}

func isValidDirectiveName(name string) bool {
	return reDirectiveName.MatchString(name)
}
