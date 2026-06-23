package introspect

import "github.com/apache/arrow-go/v18/arrow"

// RegisterCatalog adds the self-describing catalog tables `tables` and
// `columns` to r — the keelson equivalent of ClickHouse's system.tables
// / system.columns (ADR-0094 §SD8). They enumerate r's own providers at
// snapshot time, so a query can discover what is available without an
// out-of-band listing. The catalog lists itself, just as system.tables
// lists system.tables.
//
// Call it after the other providers are registered (order does not
// matter for correctness — the snapshot reflects the registry's state
// when the query runs, not when RegisterCatalog ran).
func RegisterCatalog(r *Registry) (err error) {
	if err = r.Register(&tablesProvider{reg: r}); err != nil {
		return
	}
	return r.Register(&columnsProvider{reg: r})
}

// tablesProvider exposes one row per registered table.
type tablesProvider struct{ reg *Registry }

func (*tablesProvider) Name() string             { return "tables" }
func (*tablesProvider) Freshness() FreshnessClass { return FreshnessStatic }
func (p *tablesProvider) Schema() *arrow.Schema   { return tablesTable(nil).Schema() }

func (p *tablesProvider) Snapshot(proj Projection) (arrow.RecordBatch, error) {
	ps := p.reg.Providers()
	return tablesTable(ps).Build(proj, len(ps)), nil
}

func tablesTable(ps []Provider) *Table {
	return NewTable().
		String("name", func(i int) string { return ps[i].Name() }).
		String("freshness", func(i int) string { return ps[i].Freshness().String() }).
		Int32("column_count", func(i int) int32 { return int32(ps[i].Schema().NumFields()) })
}

// columnsProvider exposes one row per (table, column).
type columnsProvider struct{ reg *Registry }

func (*columnsProvider) Name() string             { return "columns" }
func (*columnsProvider) Freshness() FreshnessClass { return FreshnessStatic }
func (p *columnsProvider) Schema() *arrow.Schema   { return columnsTable(nil).Schema() }

func (p *columnsProvider) Snapshot(proj Projection) (arrow.RecordBatch, error) {
	rows := flattenColumns(p.reg.Providers())
	return columnsTable(rows).Build(proj, len(rows)), nil
}

type columnRow struct {
	table    string
	column   string
	typ      string // the Arrow type name (e.g. "utf8", "int32", "list<...>")
	position int32
}

func flattenColumns(ps []Provider) (rows []columnRow) {
	for _, prov := range ps {
		for i, f := range prov.Schema().Fields() {
			rows = append(rows, columnRow{
				table:    prov.Name(),
				column:   f.Name,
				typ:      f.Type.String(),
				position: int32(i),
			})
		}
	}
	return
}

func columnsTable(rows []columnRow) *Table {
	return NewTable().
		String("table", func(i int) string { return rows[i].table }).
		String("column", func(i int) string { return rows[i].column }).
		String("type", func(i int) string { return rows[i].typ }).
		Int32("position", func(i int) int32 { return rows[i].position })
}
