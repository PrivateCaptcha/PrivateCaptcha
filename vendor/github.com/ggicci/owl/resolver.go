package owl

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// Saves all the built resolver trees without applying options.
// The key is the struct type.
var builtTrees sync.Map

// Resolver is a field resolver. Which is a node in the resolver tree.
// The resolver tree is built from a struct value. Each node represents a
// field in the struct. The root node represents the struct itself.
// It is used to resolve a field value from a data source.
type Resolver struct {
	Type       reflect.Type
	Field      reflect.StructField
	Index      []int
	Path       []string
	Directives []*Directive
	Parent     *Resolver
	Children   []*Resolver
	Context    context.Context // save custom resolver settings here
}

// New builds a resolver tree from a struct value. The given options will be
// applied to all the resolvers. In the resolver tree, each node is also a
// Resolver. Available options are WithNamespace, WithNestedDirectivesEnabled
// and WithValue.
func New(structValue interface{}, opts ...Option) (*Resolver, error) {
	typ, err := reflectStructType(structValue)
	if err != nil {
		return nil, err
	}

	tree, err := buildAndCacheResolverTree(typ)
	if err != nil {
		return nil, err
	}
	tree = tree.Copy()

	// Apply options, build the context for each resolver.
	defaultOpts := []Option{WithNamespace(defaultNS)}
	opts = append(defaultOpts, opts...)
	tree.applyContext(buildContextWithOptionsApplied(context.Background(), opts...))

	if tree.Namespace() == nil {
		return nil, errors.New("nil namespace")
	}

	return tree, nil
}

// Copy returns a copy of the resolver tree. The copy is a deep copy, which
// means the children are also copied.
func (r *Resolver) Copy() *Resolver {
	resolverCopy := new(Resolver)
	*resolverCopy = *r

	// Copy index and path.
	resolverCopy.Index = make([]int, len(r.Index))
	copy(resolverCopy.Index, r.Index)
	resolverCopy.Path = make([]string, len(r.Path))
	copy(resolverCopy.Path, r.Path)

	// Copy the directives.
	resolverCopy.Directives = make([]*Directive, len(r.Directives))
	for i, d := range r.Directives {
		resolverCopy.Directives[i] = d.Copy()
	}

	// Copy the children and set the parent.
	resolverCopy.Children = make([]*Resolver, len(r.Children))
	for i, child := range r.Children {
		resolverCopy.Children[i] = child.Copy()
		resolverCopy.Children[i].Parent = resolverCopy
	}
	return resolverCopy
}

func (r *Resolver) IsRoot() bool {
	return r.Parent == nil
}

func (r *Resolver) IsLeaf() bool {
	return len(r.Children) == 0
}

func (r *Resolver) PathString() string {
	return strings.Join(r.Path, ".")
}

func (r *Resolver) GetDirective(name string) *Directive {
	for _, d := range r.Directives {
		if d.Name == name {
			return d
		}
	}
	return nil
}

func (r *Resolver) RemoveDirective(name string) *Directive {
	for i, d := range r.Directives {
		if d.Name == name {
			r.Directives = append(r.Directives[:i], r.Directives[i+1:]...)
			return d
		}
	}
	return nil
}

func (r *Resolver) Namespace() *Namespace {
	return r.Context.Value(ckNamespace).(*Namespace)
}

// Find finds a field resolver by path. e.g. "Pagination.Page", "User.Name", etc.
func (r *Resolver) Lookup(path string) *Resolver {
	var paths []string
	if path != "" {
		paths = strings.Split(path, ".")
	}
	return findResolver(r, paths)
}

func findResolver(root *Resolver, path []string) *Resolver {
	if len(path) == 0 {
		return root
	}

	for _, field := range root.Children {
		if field.Field.Name == path[0] {
			return findResolver(field, path[1:])
		}
	}

	return nil
}

func shouldResolveNestedDirectives(ctx context.Context, r *Resolver) bool {
	if r.IsRoot() {
		return true // always resolve the root
	}
	if r.IsLeaf() {
		return false // leaves have no children
	}
	if len(r.Directives) == 0 {
		return true // go deeper if no directives on current field
	}
	if ctx != nil && ctx.Value(ckResolveNestedDirectives) != nil {
		return ctx.Value(ckResolveNestedDirectives).(bool)
	}
	if r.Context.Value(ckResolveNestedDirectives) != nil {
		return r.Context.Value(ckResolveNestedDirectives).(bool)
	}
	return true
}

func (r *Resolver) String() string {
	return fmt.Sprintf("%s (%v)", r.PathString(), r.Type)
}

// Iterate visits the resolver tree by depth-first. The callback function will
// be called on each field resolver. The iteration will stop if the callback
// returns an error.
func (r *Resolver) Iterate(fn func(*Resolver) error) error {
	ctx := WithValue(ckResolveNestedDirectives, true).Apply(context.Background())
	return r.iterate(ctx, fn)
}

func (root *Resolver) iterate(ctx context.Context, fn func(*Resolver) error) error {
	if err := fn(root); err != nil {
		return err
	}

	if shouldResolveNestedDirectives(ctx, root) {
		for _, field := range root.Children {
			if err := field.iterate(ctx, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyContext applies the context to the resolver and its children.
func (r *Resolver) applyContext(ctx context.Context) {
	r.Iterate(func(x *Resolver) error {
		x.Context = ctx
		return nil
	})
}

// Scan scans the struct value by traversing the fields in depth-first order. The value is required
// to have the same type as the resolver holds. While scanning, it will run the directives on each
// field. The DirectiveRuntime that can be accessed during the directive exeuction will have its
// Value property populated with reflect.Value of the field. Typically, Scan is used to do some
// readonly operations against the struct value, e.g. validate the struct value, build something
// based on the struct value, etc.
//
// Use WithValue to create an Option that can add custom values to the context, the context can be
// used by the directive executors during the scanning.
//
// NOTE: Unlike Resolve, it will iterate the whole resolver tree against the given
// value, try to access each corresponding field. Even scan fails on one of the fields,
// it will continue to scan the rest of the fields. The returned error can be a
// multi-error combined by errors.Join, which contains all the errors that occurred
// during the scan.
func (r *Resolver) Scan(value any, opts ...Option) error {
	if value == nil {
		return fmt.Errorf("cannot scan nil value")
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Type() != r.Type {
		return fmt.Errorf("%w: cannot scan value of type %q, expecting type %q",
			ErrTypeMismatch, rv.Type(), r.Type)
	}

	var errs []error
	ctx := buildContextWithOptionsApplied(context.Background(), opts...)
	r.iterate(ctx, func(r *Resolver) error {
		errs = append(errs, scan(r, ctx, rv))
		return nil
	})

	return errors.Join(errs...)
}

func scan(resolver *Resolver, ctx context.Context, rootValue reflect.Value) error {
	if resolver.IsRoot() {
		return nil // skip on root, which is the root struct itself
	}

	// Get the field value this resolver points to.
	fv, err := rootValue.FieldByIndexErr(resolver.Index)
	if err != nil {
		return &ScanError{
			fieldError: fieldError{
				Err:      fmt.Errorf("%w: %v", ErrScanNilField, err),
				Resolver: resolver,
			},
		}
	}

	// Run directives on the field.
	if err := resolver.runDirectives(ctx, fv); err != nil {
		return &ScanError{
			fieldError: fieldError{
				Err:      err,
				Resolver: resolver,
			},
		}
	}

	return nil
}

// Resolve resolves the struct type by traversing the tree in depth-first order.
// Typically it is used to create a new struct instance by reading from some
// data source. This method always creates a new value of the type the resolver
// holds. And runs the directives on each field.
//
// Use WithValue to create an Option that can add custom values to the context,
// the context can be used by the directive executors during the resolution.
// Example:
//
//	type Settings struct {
//	  DarkMode bool `owl:"env=MY_APP_DARK_MODE;cfg=appearance.dark_mode;default=false"`
//	}
//	resolver := owl.New(Settings{})
//	settings, err := resolver.Resolve(WithValue("app_config", appConfig))
//
// NOTE: while iterating the tree, if resolving a field failed, the iteration
// will be stopped immediately and the error will be returned.
func (r *Resolver) Resolve(opts ...Option) (reflect.Value, error) {
	ctx := buildContextWithOptionsApplied(context.Background(), opts...)
	rootValue := reflect.New(r.Type) // Type:User -> rootValue:*User
	return rootValue, r.resolve(ctx, rootValue)
}

// ResolveTo works like Resolve, but it resolves the struct value to the given
// pointer value instead of creating a new value. The pointer value must be
// non-nil and a pointer to the type the resolver holds.
func (r *Resolver) ResolveTo(value any, opts ...Option) (err error) {
	rv, err := reflectResolveTargetValue(value, r.Type)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResolveTarget, err)
	}
	ctx := buildContextWithOptionsApplied(context.Background(), opts...)
	return r.resolve(ctx, rv.Addr())
}

// resolve runs the directives on the current field and resolves the children fields.
// NOTE: rootValue must be a pointer to a type, i.e. *User, not User.
func (root *Resolver) resolve(ctx context.Context, rootValue reflect.Value) error {
	// Run the directives on current field.
	if err := root.runDirectives(ctx, rootValue); err != nil {
		return err
	}

	// Resolve the children fields.
	if shouldResolveNestedDirectives(ctx, root) {
		// If the root is a pointer, we need to allocate memory for it when it's
		// not instantiated yet. We only expect it's a one-level pointer, e.g.
		// *User, not **User.
		underlying := rootValue
		if root.Type.Kind() == reflect.Ptr {
			if rootValue.Elem().IsNil() { // instantiate the pointer on demand
				rootValue.Elem().Set(reflect.New(root.Type.Elem()))
			}
			underlying = rootValue.Elem()
		}
		for _, child := range root.Children {
			if err := child.resolve(ctx, underlying.Elem().Field(child.Index[len(child.Index)-1]).Addr()); err != nil {
				return &ResolveError{
					fieldError: fieldError{
						Err:      err,
						Resolver: child,
					},
				}
			}
		}
	}
	return nil
}

func (r *Resolver) runDirectives(ctx context.Context, rv reflect.Value) error {
	ns := r.Namespace()

	// The namespace can be overriden by calling Scan/Resolve with WithNamespace.
	if nsOverriden := ctx.Value(ckNamespace); nsOverriden != nil {
		ns = nsOverriden.(*Namespace)
	}

	for _, directive := range r.Directives {
		dirRuntime := &DirectiveRuntime{
			Directive: directive,
			Resolver:  r,
			Context:   ctx,
			Value:     rv,
		}
		exe := ns.LookupExecutor(directive.Name)
		if exe == nil {
			return &DirectiveExecutionError{
				Err:       ErrMissingExecutor,
				Directive: *directive,
			}
		}

		if err := exe.Execute(dirRuntime); err != nil {
			return &DirectiveExecutionError{
				Err:       err,
				Directive: *directive,
			}
		}

		ctx = dirRuntime.Context // make the context available to the next directive
	}

	return nil
}

func (r *Resolver) DebugLayoutText(depth int) string {
	var sb strings.Builder
	sb.WriteString(r.String())
	sb.WriteString(fmt.Sprintf("  %v", r.Index))

	for i, field := range r.Children {
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("    ", depth+1))
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString("# ")
		sb.WriteString(field.DebugLayoutText(depth + 1))
	}
	return sb.String()
}

// buildAndCacheResolverTree returns the tree with minimum settings (without any
// options applied). It will load from cache if possible. Otherwise, it will
// build the tree from scratch and cache it.
func buildAndCacheResolverTree(typ reflect.Type) (tree *Resolver, err error) {
	if builtTree, ok := builtTrees.Load(typ); ok { // hit cache
		return builtTree.(*Resolver), nil
	}

	tree, err = buildResolverTree(typ) // build from scratch
	if err != nil {
		return nil, err
	}

	// Build successfully, cache it.
	builtTrees.Store(typ, tree)
	return tree, nil
}

// buildResolverTree builds a resolver tree from a struct type.
func buildResolverTree(typ reflect.Type) (*Resolver, error) {
	return buildResolver(typ, reflect.StructField{}, nil)
}

func buildResolver(typ reflect.Type, field reflect.StructField, parent *Resolver) (*Resolver, error) {
	root := &Resolver{
		Type:    typ,
		Field:   field,
		Index:   []int{},
		Parent:  parent,
		Context: context.Background(),
	}

	if !root.IsRoot() {
		directives, err := parseTag(field.Tag.Get(Tag()))
		if err != nil {
			return nil, fmt.Errorf("parse directives (tag): %w", err)
		}
		root.Directives = directives
		root.Path = append(root.Parent.Path, field.Name)
		root.Index = append(root.Parent.Index, field.Index...)
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)

			// Skip unexported fields. Because we can't set value to them, nor
			// get value from them by reflection.
			if !field.IsExported() {
				continue
			}
			if field.Type == root.Type {
				continue
			}

			child, err := buildResolver(field.Type, field, root)
			if err != nil {
				path := append(root.Path, field.Name)
				return nil, fmt.Errorf("build resolver for %q failed: %w", strings.Join(path, "."), err)
			}

			// Skip the field if it has no children and no directives.
			if len(child.Children) > 0 || len(child.Directives) > 0 {
				root.Children = append(root.Children, child)
			}
		}
	}
	return root, nil
}

// parseTag creates a slice of Directive instances by parsing a struct tag.
func parseTag(tag string) ([]*Directive, error) {
	tag = strings.TrimSpace(tag)
	var directives []*Directive
	existed := make(map[string]bool)
	for _, directive := range strings.Split(tag, ";") {
		directive = strings.TrimSpace(directive)
		if directive == "" {
			continue
		}
		d, err := ParseDirective(directive)
		if err != nil {
			return nil, err
		}
		if existed[d.Name] {
			return nil, duplicateDirective(d.Name)
		}
		existed[d.Name] = true
		directives = append(directives, d)
	}
	return directives, nil
}

func reflectStructType(structValue interface{}) (reflect.Type, error) {
	typ, ok := structValue.(reflect.Type)
	if !ok {
		typ = reflect.TypeOf(structValue)
	}

	if typ == nil {
		return nil, fmt.Errorf("%w: nil type", ErrUnsupportedType)
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: non-struct type %v", ErrUnsupportedType, typ)
	}

	return typ, nil
}

func reflectResolveTargetValue(value any, expectedType reflect.Type) (rv reflect.Value, err error) {
	if value == nil {
		return rv, errors.New("nil value")
	}

	rv = reflect.ValueOf(value)
	if rv.Kind() != reflect.Pointer {
		return rv, errors.New("non-pointer value")
	}

	if rv, err = dereference(rv); err != nil {
		return rv, errors.New("nil pointer value")
	}

	if rv.Type() != expectedType {
		return rv, fmt.Errorf("%w: cannot resolve to value of type %q, expecting type %q",
			ErrTypeMismatch, rv.Type(), expectedType)
	}

	return rv, nil
}

// dereference returns the value that v points to, or an error if v is nil.
// It can be multiple levels deep. e.g. T -> T, *T -> T; **T -> T, etc.
func dereference(v reflect.Value) (reflect.Value, error) {
	if v.Kind() != reflect.Pointer {
		return v, nil
	}
	if v.IsNil() {
		return v, errors.New("nil pointer")
	}
	return dereference(v.Elem())
}
