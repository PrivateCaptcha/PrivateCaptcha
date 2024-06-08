package owl

import "fmt"

var (
	defaultNS = NewNamespace()
)

// RegisterDirectiveExecutor registers a named executor globally, i.e. to the default
// namespace.
func RegisterDirectiveExecutor(name string, exe DirectiveExecutor, replace ...bool) {
	defaultNS.RegisterDirectiveExecutor(name, exe, replace...)
}

// LookupExecutor returns the executor by name globally (from the default
// namespace).
func LookupExecutor(name string) DirectiveExecutor {
	return defaultNS.LookupExecutor(name)
}

// Namespace isolates the executors as a collection.
type Namespace struct {
	executors map[string]DirectiveExecutor
}

// NewNamespace creates a new namespace. Which is a collection of executors.
func NewNamespace() *Namespace {
	return &Namespace{
		executors: make(map[string]DirectiveExecutor),
	}
}

// RegisterDirectiveExecutor registers a named executor to the namespace. The executor
// should implement the DirectiveExecutor interface. Will panic if the name were taken
// or the executor is nil. Pass replace (true) to ignore the name conflict.
func (ns *Namespace) RegisterDirectiveExecutor(name string, exe DirectiveExecutor, replace ...bool) {
	force := len(replace) > 0 && replace[0]
	if _, ok := ns.executors[name]; ok && !force {
		panic(fmt.Errorf("owl: %s", duplicateExecutor(name)))
	}
	if exe == nil {
		panic(fmt.Errorf("owl: %s", nilExecutor(name)))
	}
	if !isValidDirectiveName(name) {
		panic(fmt.Errorf("owl: %s", invalidDirectiveName(name)))
	}
	ns.executors[name] = exe
}

// LookupExecutor returns the executor by name.
func (ns *Namespace) LookupExecutor(name string) DirectiveExecutor {
	return ns.executors[name]
}
