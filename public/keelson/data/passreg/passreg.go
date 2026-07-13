// Package passreg is a process-level registry of nanopass SQL passes
// (ADR-0108). Pass producers stay unaware of it; an aggregator (see the
// defaults subpackage) registers their passes at host wiring time, keyed
// by StageE — a semantic execution point. Executors of user-authored SQL
// then apply a stage's entries either strictly (Compose) or best-effort
// (ApplyBestEffort). The registry also feeds the keelson.sql_passes
// introspection table (ADR-0094).
package passreg

import (
	"sort"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// StageE names a semantic execution point in the life of a SQL statement.
// Consumers sitting at the same point apply the same entries — that is the
// seam's contract: a pass registered once behaves identically in every
// executor of that stage. New stages arrive with their first consumer, not
// speculatively (ADR-0108 §SD2).
type StageE uint8

const (
	// StageInvalid is the zero value; registering an entry with it is an
	// error, which catches uninitialised Entry literals.
	StageInvalid StageE = iota
	// StagePreExecute marks rewrites applied to user-authored SQL
	// immediately before it ships to an executor (a remote ClickHouse
	// server or the chlocal pool). Body-only rewrites; editor and preview
	// surfaces keep showing the user's original text.
	StagePreExecute
)

func (inst StageE) String() (s string) {
	switch inst {
	case StagePreExecute:
		s = "pre-execute"
	default:
		s = "invalid"
	}
	return
}

// knownStage reports whether s is a registrable stage.
func knownStage(s StageE) (ok bool) {
	switch s {
	case StagePreExecute:
		ok = true
	}
	return
}

// Entry is one registered pass plus the metadata that composition order
// and the catalog need.
type Entry struct {
	Pass nanopass.Pass
	// Stage is the execution point the pass applies at.
	Stage StageE
	// Order sorts entries within a stage; ties break by Pass.Name. Leave
	// gaps (steps of 100) so later registrants can slot in between.
	Order int
	// Description is one line for the catalog.
	Description string
	// Provenance is the import path of the package providing the pass.
	Provenance string
}

// Factory is a pass whose concrete nanopass.Pass is produced per consumer
// from a binding the registry does not itself hold — a per-client schema
// resolver (ADR-0116), a per-request database. It is the "second entry kind"
// ADR-0108 §SD7 anticipated: registered once so it appears in the catalog,
// and realised at each consumer's application site via Build. Concrete Entry
// values stay the common case; reach for a Factory only when the pass cannot
// be a process-global value.
type Factory struct {
	// Name identifies the factory within its stage and names its catalog
	// row; it must equal the realised Pass.Name.
	Name string
	// Stage is the execution point the realised pass applies at.
	Stage StageE
	// Order sorts the realised pass among a stage's units; ties break by
	// Name. Leave gaps (steps of 100) as with Entry.
	Order int
	// Description is one line for the catalog.
	Description string
	// Provenance is the import path of the package providing the pass.
	Provenance string
	// Properties is the realised pass's behavioural metadata, declared
	// statically so the catalog can describe a factory without binding it;
	// it must match what Build's pass carries.
	Properties nanopass.PassProperties
	// Build realises the concrete pass for binding, returning ok=false when
	// the binding does not carry what this factory needs — the consumer then
	// skips it, and it stays a catalog-only descriptor for that consumer.
	Build func(binding any) (pass nanopass.Pass, ok bool)
}

type entryKey struct {
	stage StageE
	name  string
}

// Registry holds pass entries and factories keyed by (stage, name), a single
// namespace across both kinds. The zero value is unusable; build one with
// NewRegistry.
type Registry struct {
	mu        sync.RWMutex
	entries   map[entryKey]Entry
	factories map[entryKey]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		entries:   make(map[entryKey]Entry),
		factories: make(map[entryKey]Factory),
	}
}

// Register adds e. The stage must be known and the pass must carry a name
// and an Apply; a duplicate (stage, name) is an error and the first
// registration stays.
func (inst *Registry) Register(e Entry) (err error) {
	if !knownStage(e.Stage) {
		err = eh.Errorf("passreg: unknown stage %d (pass %q)", e.Stage, e.Pass.Name)
		return
	}
	if e.Pass.Name == "" {
		err = eh.Errorf("passreg: pass has no name (stage %s)", e.Stage)
		return
	}
	if e.Pass.Apply == nil {
		err = eh.Errorf("passreg: pass %q has nil Apply", e.Pass.Name)
		return
	}
	k := entryKey{stage: e.Stage, name: e.Pass.Name}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, exists := inst.entries[k]; exists {
		err = eh.Errorf("passreg: pass %q already registered at stage %s", e.Pass.Name, e.Stage)
		return
	}
	if _, exists := inst.factories[k]; exists {
		err = eh.Errorf("passreg: name %q at stage %s already registered as a factory", e.Pass.Name, e.Stage)
		return
	}
	inst.entries[k] = e
	return
}

// RegisterFactory adds f. The stage must be known, the name non-empty, and
// Build non-nil; the (stage, name) key must be free across both factories and
// concrete entries. A duplicate is an error and the first registration stays.
func (inst *Registry) RegisterFactory(f Factory) (err error) {
	if !knownStage(f.Stage) {
		err = eh.Errorf("passreg: unknown stage %d (factory %q)", f.Stage, f.Name)
		return
	}
	if f.Name == "" {
		err = eh.Errorf("passreg: factory has no name (stage %s)", f.Stage)
		return
	}
	if f.Build == nil {
		err = eh.Errorf("passreg: factory %q has nil Build", f.Name)
		return
	}
	k := entryKey{stage: f.Stage, name: f.Name}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, exists := inst.factories[k]; exists {
		err = eh.Errorf("passreg: factory %q already registered at stage %s", f.Name, f.Stage)
		return
	}
	if _, exists := inst.entries[k]; exists {
		err = eh.Errorf("passreg: name %q at stage %s already registered as an entry", f.Name, f.Stage)
		return
	}
	inst.factories[k] = f
	return
}

// Entries returns the stage's entries sorted by (Order, Pass.Name). The
// registration order across wiring sites is deliberately not trusted —
// composition must be deterministic across processes. The slice is a copy.
func (inst *Registry) Entries(stage StageE) (es []Entry) {
	inst.mu.RLock()
	es = make([]Entry, 0, len(inst.entries))
	for k, e := range inst.entries {
		if k.stage == stage {
			es = append(es, e)
		}
	}
	inst.mu.RUnlock()
	sortEntries(es)
	return
}

// Factories returns the stage's factory descriptors sorted by (Order, Name).
// The slice is a copy. Consumers holding a binding realise these via
// ApplyBestEffortBound; the catalog lists them via Catalog.
func (inst *Registry) Factories(stage StageE) (fs []Factory) {
	inst.mu.RLock()
	for k, f := range inst.factories {
		if k.stage == stage {
			fs = append(fs, f)
		}
	}
	inst.mu.RUnlock()
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Order != fs[j].Order {
			return fs[i].Order < fs[j].Order
		}
		return fs[i].Name < fs[j].Name
	})
	return
}

// All returns every concrete entry sorted by (Stage, Order, Pass.Name).
// Catalog is the fuller view — entries plus factory descriptors — that the
// sql_passes provider consumes.
func (inst *Registry) All() (es []Entry) {
	inst.mu.RLock()
	es = make([]Entry, 0, len(inst.entries))
	for _, e := range inst.entries {
		es = append(es, e)
	}
	inst.mu.RUnlock()
	sortEntries(es)
	return
}

// CatalogRow is the read model for the keelson.sql_passes table (ADR-0108
// §SD5): one row per registered unit — a concrete Entry or a late-bound
// Factory — so the catalog reflects every rewrite the process can apply,
// including per-consumer factories.
type CatalogRow struct {
	Stage       StageE
	Name        string
	Order       int
	Description string
	Provenance  string
	Properties  nanopass.PassProperties
	// LateBound marks a Factory descriptor: it applies only where a consumer
	// supplies a binding (ApplyBestEffortBound), never on the unbound path.
	LateBound bool
}

// Catalog returns every registered unit — concrete entries and factory
// descriptors — as catalog rows sorted by (Stage, Order, Name).
func (inst *Registry) Catalog() (rows []CatalogRow) {
	inst.mu.RLock()
	rows = make([]CatalogRow, 0, len(inst.entries)+len(inst.factories))
	for _, e := range inst.entries {
		rows = append(rows, CatalogRow{
			Stage:       e.Stage,
			Name:        e.Pass.Name,
			Order:       e.Order,
			Description: e.Description,
			Provenance:  e.Provenance,
			Properties:  e.Pass.Properties,
			LateBound:   false,
		})
	}
	for _, f := range inst.factories {
		rows = append(rows, CatalogRow{
			Stage:       f.Stage,
			Name:        f.Name,
			Order:       f.Order,
			Description: f.Description,
			Provenance:  f.Provenance,
			Properties:  f.Properties,
			LateBound:   true,
		})
	}
	inst.mu.RUnlock()
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Name < b.Name
	})
	return
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Pass.Name < b.Pass.Name
	})
}

// Compose returns the stage's entries as one strict nanopass.Sequence:
// shared env, first error aborts. With no entries it is a pass-through.
// For executors of user-authored SQL, ApplyBestEffort is usually the
// right mode instead.
func (inst *Registry) Compose(name string, stage StageE) (p nanopass.Pass) {
	es := inst.Entries(stage)
	ps := make([]nanopass.Pass, 0, len(es))
	for _, e := range es {
		ps = append(ps, e.Pass)
	}
	p = nanopass.Sequence(name, ps...)
	return
}

// ApplyBestEffort applies the stage's entries in order, each via its own
// Pass.Run. An entry that errors is logged at warn level and skipped — the
// SQL from before it is kept and later entries still run. User-authored
// SQL may legitimately exceed Grammar1, so a rewrite failure must never
// block executing otherwise-valid SQL (ADR-0108 §SD3). Entries round-trip
// independently (no shared env); consumers needing shared-env semantics
// use Compose.
func (inst *Registry) ApplyBestEffort(stage StageE, sql string, logger zerolog.Logger) (out string) {
	out = sql
	for _, e := range inst.Entries(stage) {
		next, runErr := e.Pass.Run(out)
		if runErr != nil {
			logger.Warn().Err(runErr).Str("pass", e.Pass.Name).Str("stage", stage.String()).Msg("passreg: pass failed; skipped")
			continue
		}
		out = next
	}
	return
}

// ApplyBestEffortBound is ApplyBestEffort extended with factory realisation:
// the stage's concrete entries and every Factory whose Build accepts binding
// apply together, in (Order, Name) order, each best-effort (a failing unit is
// logged at warn level and skipped, the prior SQL kept). A Factory whose Build
// declines the binding is skipped — so a consumer lacking the binding a factory
// needs simply does not apply it. The plain ApplyBestEffort applies no factories
// at all: use it where there is no binding (the introspection /query path) and
// this where there is (play binds its leeway schema resolver, ADR-0116 §SD6).
func (inst *Registry) ApplyBestEffortBound(stage StageE, sql string, binding any, logger zerolog.Logger) (out string) {
	out = sql
	for _, u := range inst.boundUnits(stage, binding) {
		next, runErr := u.pass.Run(out)
		if runErr != nil {
			logger.Warn().Err(runErr).Str("pass", u.name).Str("stage", stage.String()).Msg("passreg: pass failed; skipped")
			continue
		}
		out = next
	}
	return
}

// boundUnit is one appliable pass — from a concrete entry or a realised
// factory — with the keys that order it within a stage.
type boundUnit struct {
	order int
	name  string
	pass  nanopass.Pass
}

// boundUnits merges the stage's concrete entries with the factories that
// accept binding into one (Order, Name)-sorted apply list. Factories are built
// after the read lock is released: Build may be non-trivial and must not run
// under inst.mu.
func (inst *Registry) boundUnits(stage StageE, binding any) (us []boundUnit) {
	inst.mu.RLock()
	for k, e := range inst.entries {
		if k.stage == stage {
			us = append(us, boundUnit{order: e.Order, name: e.Pass.Name, pass: e.Pass})
		}
	}
	facs := make([]Factory, 0, len(inst.factories))
	for k, f := range inst.factories {
		if k.stage == stage {
			facs = append(facs, f)
		}
	}
	inst.mu.RUnlock()
	for _, f := range facs {
		pass, ok := f.Build(binding)
		if !ok {
			continue
		}
		us = append(us, boundUnit{order: f.Order, name: f.Name, pass: pass})
	}
	sort.Slice(us, func(i, j int) bool {
		if us[i].order != us[j].order {
			return us[i].order < us[j].order
		}
		return us[i].name < us[j].name
	})
	return
}

// Default is the process-wide registry hosts wire at boot; the defaults
// subpackage carries the standard set (ADR-0108 §SD4).
var Default = NewRegistry()

// Register adds e to the Default registry.
func Register(e Entry) error { return Default.Register(e) }

// Entries returns the Default registry's entries for stage, sorted.
func Entries(stage StageE) []Entry { return Default.Entries(stage) }
