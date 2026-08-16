package providers

import (
	"sort"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// lwComponentsProvider publishes the component kinds this process can read
// through LW_COMPONENT, as keelson.lw_components (ADR-0189 §SD8).
//
// It is the discovery half of the component authoring surface. Without it the
// only way to learn which kinds resolve is to write a call and read the
// refusal, which lists them — a diagnostic standing in for a catalogue. The
// gap is the one keelson.memberships closes for ref-membership ids, in a
// milder form: a kind is at least a name a person can guess, where an id is
// not.
//
// # Why the registry rather than the link set
//
// The rows are what a host registered (ADR-0189 §SD7), not what the binary
// links. That is the honest answer to "what can I query here": a store whose
// package is linked but never registered resolves nothing, and a table
// sourced from the link set would promise otherwise.
//
// Freshness is Live for the same reason keelson.sql_passes is: registration
// happens at wiring time in practice, but a late one must not read as absent.
//
// # Why sizes rather than the SQL
//
// The artefacts are large — a projection runs tens of kilobytes, and thirteen
// of them would make this table a payload rather than a catalogue. A reader
// who wants the SQL writes LW_COMPONENT and lets the pass expand it, which is
// the whole point of the surface; the byte counts are here so "there is a
// projection for this kind" is answerable without shipping it.
type lwComponentsProvider struct{}

func (lwComponentsProvider) Name() string                         { return "lw_components" }
func (lwComponentsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (lwComponentsProvider) Schema() *arrow.Schema                { return lwComponentsTable(nil).Schema() }

func (lwComponentsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := lwComponentRows()
	return lwComponentsTable(rows).Build(proj, len(rows)), nil
}

type lwComponentRow struct {
	kind       string
	store      string
	table      string
	presence   int32
	validator  int32
	filter     int32
	projection int32
}

// lwComponentRows reads the process's component registry, sorted by kind so
// the table is stable across runs — an introspection table that reordered
// itself between queries would be unusable as a diff target.
func lwComponentRows() (rows []lwComponentRow) {
	kinds := componentsql.Default.Kinds()
	rows = make([]lwComponentRow, 0, len(kinds))
	for _, kind := range kinds {
		b, ok := componentsql.Default.Lookup(kind)
		if !ok {
			// Kinds() and Lookup() read the same map under the same lock, so
			// this is unreachable; skipping rather than asserting keeps a
			// future concurrent registration from panicking a query.
			continue
		}
		rows = append(rows, lwComponentRow{
			kind:       b.Kind,
			store:      b.Store,
			table:      b.Table,
			presence:   int32(len(b.Presence)),
			validator:  int32(len(b.Validator)),
			filter:     int32(len(b.Filter)),
			projection: int32(len(b.Projection)),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].kind < rows[j].kind })
	return
}

func lwComponentsTable(rows []lwComponentRow) *introspect.Table {
	return introspect.NewTable().
		// The name a call carries: LW_COMPONENT('<kind>').
		String("kind", func(i int) string { return rows[i].kind }).
		String("store", func(i int) string { return rows[i].store }).
		// The table the kind's artefacts read. Their column references are
		// unqualified (ADR-0189 §SD6), so this is also the FROM a component
		// read must name — the pass refuses a SELECT over anything else.
		String("table", func(i int) string { return rows[i].table }).
		// Sizes in bytes of the four ADR-0066 artefacts. Filter is what
		// LW_COMPONENT_FILTER expands to and what LW_COMPONENT adds to the
		// WHERE; projection is the named tuple it selects.
		Int32("presence_bytes", func(i int) int32 { return rows[i].presence }).
		Int32("validator_bytes", func(i int) int32 { return rows[i].validator }).
		Int32("filter_bytes", func(i int) int32 { return rows[i].filter }).
		Int32("projection_bytes", func(i int) int32 { return rows[i].projection })
}
