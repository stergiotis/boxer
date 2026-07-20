package introspect

import (
	"sort"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Registry holds introspection table providers keyed by table name.
// The zero value is unusable; build one with NewRegistry.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Provider)}
}

// Register adds p. The table name must be a valid identifier and unique
// in the registry; a duplicate is an error and the first registration
// is left in place.
func (r *Registry) Register(p Provider) (err error) {
	name := p.Name()
	if !validTableName(name) {
		return eh.Errorf("introspect: invalid table name %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return eh.Errorf("introspect: table %q already registered", name)
	}
	r.byName[name] = p
	return
}

// Unregister removes the provider for name if present, reporting whether
// one was removed. It is the counterpart to Register for ephemeral
// entries (ADR-0134): an ad-hoc dataset is retracted by unregistering
// its handle, so the name stops resolving and the namespace does not
// leak. System introspection providers are registered once at startup
// and never unregistered.
func (r *Registry) Unregister(name string) (removed bool) {
	r.mu.Lock()
	if _, ok := r.byName[name]; ok {
		delete(r.byName, name)
		removed = true
	}
	r.mu.Unlock()
	return
}

// Lookup returns the provider for table name.
func (r *Registry) Lookup(name string) (p Provider, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok = r.byName[name]
	return
}

// Names returns the registered table names, sorted.
func (r *Registry) Names() (names []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names = make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return
}

// Providers returns all providers, sorted by table name.
func (r *Registry) Providers() (ps []Provider) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	ps = make([]Provider, 0, len(names))
	for _, n := range names {
		ps = append(ps, r.byName[n])
	}
	return
}

// Default is the process-wide registry that subsystems register into.
var Default = NewRegistry()

// Register adds p to the Default registry.
func Register(p Provider) error { return Default.Register(p) }

// Unregister removes name from the Default registry.
func Unregister(name string) bool { return Default.Unregister(name) }

// validTableName reports whether name is a safe ClickHouse identifier
// for use as a TEMPORARY table name and a URL path segment:
// `[A-Za-z_][A-Za-z0-9_]*`, up to 64 bytes (ADR-0094 §SD1). It matches
// the chlocalbroker InputTables rule so a name that registers here also
// passes the broker.
func validTableName(name string) (ok bool) {
	if name == "" || len(name) > 64 {
		return
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		valid := c == '_' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(i > 0 && c >= '0' && c <= '9')
		if !valid {
			return
		}
	}
	ok = true
	return
}
