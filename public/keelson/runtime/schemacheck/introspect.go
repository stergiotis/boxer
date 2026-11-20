package schemacheck

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// TableName is keelson('schema_tables'): one row per stored table the
// process judged, with its verdict.
const TableName = "schema_tables"

// RegisterIntrospect registers keelson('schema_tables') over board. A nil
// board answers with an empty table.
func RegisterIntrospect(reg *introspect.Registry, board *Board) (err error) {
	return reg.Register(tablesProvider{board: board})
}

type tablesProvider struct{ board *Board }

func (tablesProvider) Name() string                         { return TableName }
func (tablesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (tablesProvider) Schema() *arrow.Schema                { return verdictTable(nil).Schema() }

func (p tablesProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	vs := p.board.Verdicts()
	rec = verdictTable(vs).Build(proj, len(vs))
	return
}

func verdictTable(vs []recordstore.SchemaVerdict) *introspect.Table {
	renames := func(i int) (out []string) {
		for _, r := range vs[i].Renames {
			out = append(out, r.From+" -> "+r.To)
		}
		return
	}
	return introspect.NewTable().
		String("table", func(i int) string { return vs[i].Table }).
		String("kind", func(i int) string { return vs[i].Kind }).
		String("verdict", func(i int) string { return vs[i].Verdict.String() }).
		// Which schema wrote the table, as its stamp says: same, older,
		// newer, other, or unstamped.
		String("stamp_age", func(i int) string { return vs[i].Age.String() }).
		String("summary", func(i int) string { return vs[i].Summary() }).
		StringList("missing", func(i int) []string { return vs[i].Missing }).
		StringList("renames", renames).
		StringList("extra", func(i int) []string { return vs[i].Extra }).
		String("reasons", func(i int) string { return strings.Join(vs[i].Reasons, "; ") }).
		String("notes", func(i int) string { return strings.Join(vs[i].Notes, "; ") }).
		String("stamp", func(i int) string { return vs[i].Stamp }).
		String("engine", func(i int) string { return vs[i].Engine }).
		Bool("dedup_window", func(i int) bool { return vs[i].DedupWindow })
}
