// Package componentsql publishes the ADR-0066 read-back artefacts a component
// definition generates, and the registry a SQL-authoring surface looks them up
// in (ADR-0189).
//
// The artefacts are produced by
// [github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback]
// and baked into a generated record store. Until this package existed the
// store kept only the Filter, unexported, so the predicate a component
// definition produces was reachable only by copying it out of generated code.
//
// This package holds no leeway or storage dependency of its own: it is the
// leaf both a generated store and a nanopass pass can import.
//
// # The one property a caller must know
//
// [Artefacts.Projection] alone is **not** the exact read. It locates an
// attribute with indexOf, so under a membership carried by more than one
// attribute it silently returns the first; [Artefacts.Validator] is what
// rejects that row and [Artefacts.Filter] is the form carrying both
// (ADR-0066's 2026-07-27 Update). A caller embedding Projection without
// Filter gets first-match semantics, not conformance — a wrong answer that
// looks like a right one. ADR-0189 §SD4 is why the expansion pass injects the
// Filter rather than trusting each author to remember this.
package componentsql

import (
	"sort"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Artefacts are the ClickHouse read-back fragments generated for one component
// kind. Each is a SQL expression over the bound table's physical columns, with
// membership ids baked as literals at generation time.
type Artefacts struct {
	// Presence is a necessary-but-not-sufficient prefilter: no false
	// negatives, and the only index-eligible part. Its has/hasAll terms are
	// what ClickHouse prunes granules for through a bloom-filter skip index,
	// which it never does for Validator's countEqual — so Presence earns its
	// keep only in a WHERE clause, never wrapped in an expression.
	Presence string
	// Validator is the exact conformance check: semantically complete on its
	// own and index-blind, so embedding it without Presence forces a scan.
	Validator string
	// Filter is Presence AND Validator — the form to embed in WHERE, and the
	// one that is both exact and prunable. It uses ClickHouse built-ins only
	// (has / hasAll / countEqual), so it needs no UDF installed.
	Filter string
	// Projection is a CAST to a named Tuple extracting every field, so a
	// caller addresses slots by their Go field name. Unlike Filter it
	// references the leeway read-back helper UDFs, so it needs them installed.
	// See the package doc for why it must not travel without Filter.
	Projection string
}

// Set is one generated store's published artefacts: every component kind it
// carries, over the one table it binds.
type Set struct {
	// Store names the publishing store, for diagnostics — it is what a
	// collision message points at.
	Store string
	// Table is the database-qualified table the artefacts' column references
	// resolve against. The column names are unqualified, so a consumer must
	// bind them to this table itself (ADR-0189 §SD6).
	Table string
	// Kinds maps the component's Go type name to its artefacts.
	Kinds map[string]Artefacts
}

// Binding is what a lookup answers: one kind's artefacts and the table they
// read.
type Binding struct {
	Artefacts
	Kind  string
	Store string
	Table string
}

// Registry resolves a component kind to its artefacts. Hosts populate it
// explicitly at their wiring site rather than by package init: a registry
// filled by init has a link-set-dependent extent, and an authoring surface
// that silently knows about fewer kinds in one binary than another is worse
// than one line of wiring (ADR-0189 §SD7).
type Registry struct {
	mu    sync.RWMutex
	kinds map[string]Binding
}

// Default is the registry a host registers into unless it is building its own.
var Default = NewRegistry()

// NewRegistry returns an empty registry.
func NewRegistry() (inst *Registry) {
	inst = &Registry{kinds: make(map[string]Binding)}
	return
}

// Register adds every kind in set.
//
// A kind already registered is refused rather than overwritten, and the error
// names both publishers: two stores whose component types collide would
// otherwise make a query's meaning depend on wiring order. The refusal is the
// same posture ADR-0121's condition pass takes on a name collision.
//
// Registration is all-or-nothing: a set carrying one bad kind adds none of
// them, so a failed Register leaves the registry as it was.
func (inst *Registry) Register(set Set) (err error) {
	if set.Table == "" {
		err = eb.Build().Str("store", set.Store).Errorf("componentsql: set has no table")
		return
	}
	if len(set.Kinds) == 0 {
		err = eb.Build().Str("store", set.Store).Str("table", set.Table).
			Errorf("componentsql: set carries no kinds")
		return
	}

	staged := make(map[string]Binding, len(set.Kinds))
	for _, kind := range sortedKeys(set.Kinds) {
		a := set.Kinds[kind]
		if a.Filter == "" || a.Projection == "" {
			err = eb.Build().Str("store", set.Store).Str("kind", kind).
				Errorf("componentsql: kind has no filter or no projection; a set that cannot answer is worse than an absent one")
			return
		}
		inst.mu.RLock()
		prev, dup := inst.kinds[kind]
		inst.mu.RUnlock()
		if dup {
			err = eb.Build().Str("kind", kind).Str("store", set.Store).
				Str("alreadyRegisteredBy", prev.Store).Str("table", prev.Table).
				Errorf("componentsql: two stores publish the same component kind; a query naming it would resolve by wiring order")
			return
		}
		staged[kind] = Binding{Artefacts: a, Kind: kind, Store: set.Store, Table: set.Table}
	}

	inst.mu.Lock()
	defer inst.mu.Unlock()
	// Re-check under the write lock: the staging pass read with RLock, so a
	// concurrent Register could have claimed a kind in between.
	for kind := range staged {
		if prev, dup := inst.kinds[kind]; dup {
			err = eb.Build().Str("kind", kind).Str("store", set.Store).
				Str("alreadyRegisteredBy", prev.Store).
				Errorf("componentsql: two stores publish the same component kind")
			return
		}
	}
	for kind, b := range staged {
		inst.kinds[kind] = b
	}
	return
}

// MustRegister is Register for a wiring site that cannot proceed without it.
func (inst *Registry) MustRegister(set Set) {
	err := inst.Register(set)
	if err != nil {
		panic(err)
	}
}

// Lookup resolves a component kind. ok is false for a kind no registered store
// publishes — which a caller should report as an error naming the kind, never
// expand to nothing.
func (inst *Registry) Lookup(kind string) (b Binding, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	b, ok = inst.kinds[kind]
	return
}

// Kinds returns every registered kind, sorted — for diagnostics, for the
// vocabulary panel, and so a "no such kind" message can list the alternatives.
func (inst *Registry) Kinds() (kinds []string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	kinds = make([]string, 0, len(inst.kinds))
	for kind := range inst.kinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return
}

// Reset empties the registry. For tests and for a host rebuilding its wiring;
// a served registry is written once at startup.
func (inst *Registry) Reset() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.kinds = make(map[string]Binding)
}

// Register adds set to [Default].
func Register(set Set) error { return Default.Register(set) }

// Lookup resolves kind in [Default].
func Lookup(kind string) (Binding, bool) { return Default.Lookup(kind) }

func sortedKeys(m map[string]Artefacts) (keys []string) {
	keys = make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return
}
