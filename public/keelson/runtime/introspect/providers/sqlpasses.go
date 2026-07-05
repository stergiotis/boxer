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
// which order, is a query rather than a source dive. Registration
// happens at host wiring time in practice, but Live keeps a late
// registration honest; the table is tiny.
type sqlPassesProvider struct{}

func (sqlPassesProvider) Name() string                         { return "sql_passes" }
func (sqlPassesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (sqlPassesProvider) Schema() *arrow.Schema                { return sqlPassesTable(nil).Schema() }

func (sqlPassesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	es := passreg.Default.All()
	return sqlPassesTable(es).Build(proj, len(es)), nil
}

func sqlPassesTable(es []passreg.Entry) *introspect.Table {
	return introspect.NewTable().
		String("stage", func(i int) string { return es[i].Stage.String() }).
		String("name", func(i int) string { return es[i].Pass.Name }).
		Int32("apply_order", func(i int) int32 { return int32(es[i].Order) }).
		Bool("idempotent", func(i int) bool { return es[i].Pass.Properties.Idempotent }).
		Bool("needs_fixed_point", func(i int) bool { return es[i].Pass.Properties.NeedsFixedPoint }).
		StringList("reads", func(i int) []string { return regionNames(es[i].Pass.Properties.Reads) }).
		StringList("writes", func(i int) []string { return regionNames(es[i].Pass.Properties.Writes) }).
		String("provenance", func(i int) string { return es[i].Provenance }).
		String("description", func(i int) string { return es[i].Description })
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
