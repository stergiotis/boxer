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

type entryKey struct {
	stage StageE
	name  string
}

// Registry holds pass entries keyed by (stage, pass name). The zero value
// is unusable; build one with NewRegistry.
type Registry struct {
	mu      sync.RWMutex
	entries map[entryKey]Entry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[entryKey]Entry)}
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
	inst.entries[k] = e
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

// All returns every entry sorted by (Stage, Order, Pass.Name). The catalog
// provider consumes this.
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

// Default is the process-wide registry hosts wire at boot; the defaults
// subpackage carries the standard set (ADR-0108 §SD4).
var Default = NewRegistry()

// Register adds e to the Default registry.
func Register(e Entry) error { return Default.Register(e) }

// Entries returns the Default registry's entries for stage, sorted.
func Entries(stage StageE) []Entry { return Default.Entries(stage) }
