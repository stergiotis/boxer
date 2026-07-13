package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// sqlPassesProvider exposes the registered nanopass SQL passes as
// keelson.sql_passes (ADR-0108 §SD5) — the catalog half of the pass
// registry: which rewrites this process applies, at which stage, in
// which order, is a query rather than a source dive. It includes late-bound
// Factory descriptors (late_bound=true, ADR-0108 §SD7), which apply only
// where a consumer supplies a binding — so a per-client rewrite such as
// play's leeway name resolver still appears here. Registration happens at
// host wiring time in practice, but Live keeps a late registration honest;
// the table is tiny.
type sqlPassesProvider struct{}

func (sqlPassesProvider) Name() string                         { return "sql_passes" }
func (sqlPassesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (sqlPassesProvider) Schema() *arrow.Schema                { return sqlPassesTable(nil).Schema() }

func (sqlPassesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := passreg.Default.Catalog()
	return sqlPassesTable(rows).Build(proj, len(rows)), nil
}

func sqlPassesTable(rows []passreg.CatalogRow) *introspect.Table {
	return introspect.NewTable().
		String("stage", func(i int) string { return rows[i].Stage.String() }).
		String("name", func(i int) string { return rows[i].Name }).
		Int32("apply_order", func(i int) int32 { return int32(rows[i].Order) }).
		Bool("late_bound", func(i int) bool { return rows[i].LateBound }).
		Bool("idempotent", func(i int) bool { return rows[i].Properties.Idempotent }).
		Bool("needs_fixed_point", func(i int) bool { return rows[i].Properties.NeedsFixedPoint }).
		StringList("reads", func(i int) []string { return regionNames(rows[i].Properties.Reads) }).
		StringList("writes", func(i int) []string { return regionNames(rows[i].Properties.Writes) }).
		String("provenance", func(i int) string { return rows[i].Provenance }).
		String("description", func(i int) string { return rows[i].Description })
}

// regionNames renders an EnvRegions bitset as sorted-by-declaration
// region names (nanopass exports no String for it).
func regionNames(r nanopass.EnvRegions) (out []string) {
	out = make([]string, 0, 5)
	for _, x := range []struct {
		bit  nanopass.EnvRegions
		name string
	}{
		{nanopass.RegionBody, "body"},
		{nanopass.RegionSessionSettings, "session_settings"},
		{nanopass.RegionStatementSettings, "statement_settings"},
		{nanopass.RegionParams, "params"},
		{nanopass.RegionFormat, "format"},
	} {
		if r&x.bit != 0 {
			out = append(out, x.name)
		}
	}
	return
}
