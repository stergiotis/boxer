package chviews

import (
	"fmt"
)

// Name shapes, as leeway.tables reports them. Each is a count of column
// layouts, not a classification of the table (ADR-0226 §SD2).
const (
	// NameShapeEmpty is a table the decode found no columns for.
	NameShapeEmpty = "empty"
	// NameShapeForeign is a table no column name of which decoded.
	NameShapeForeign = "foreign"
	// NameShapeMixed is a table some of whose column names decoded. A leeway
	// table with a hand-added or MATERIALIZED column reads this way, and that
	// is not a fault.
	NameShapeMixed = "mixed"
	// NameShapeLeeway is a table every column name of which decoded — which is
	// necessary for discovery to succeed and not sufficient for it. The
	// verdict is boxer.tables_leeway's (ADR-0170).
	NameShapeLeeway = "leeway"
)

// tablePassthroughColumns are carried from system.tables unchanged. The
// engine-level size and key columns are the ones a schema question reaches
// for; create_table_query is deliberately absent — it is the whole DDL, and a
// view nobody can read at the terminal is a view nobody selects from.
var tablePassthroughColumns = []string{
	"engine",
	"total_rows",
	"total_bytes",
	"comment",
	"partition_key",
	"sorting_key",
	"primary_key",
}

// tableAggregateColumns are what the decode's per-table aggregate contributes,
// in the order the view presents them. name_shape is not here: it is computed
// in the outer select from these counts, not carried up from the aggregate.
var tableAggregateColumns = []string{
	"n_columns",
	"n_plain_columns",
	"n_tagged_columns",
	"n_foreign_columns",
	"sections",
	"n_sections",
	"table_row_config",
	"n_table_row_configs",
	"streaming_groups",
	"data_compressed_bytes",
	"data_uncompressed_bytes",
}

// TablesColumns returns the view's result columns in order.
func TablesColumns() (names []string) {
	names = make([]string, 0, 3+len(tablePassthroughColumns)+len(tableAggregateColumns))
	names = append(names, "database", "name")
	names = append(names, tablePassthroughColumns...)
	names = append(names, "name_shape")
	names = append(names, tableAggregateColumns...)
	return
}

// tablesBody joins the decode's per-table aggregate onto system.tables.
//
// join_use_nulls is pinned because name_shape reads the aggregate's counts: a
// table the aggregate has no row for must arrive as zero, not as NULL, or the
// one case the empty shape exists to name reports as no shape at all.
func tablesBody(target TargetDatabase) (sql string) {
	notForeign := fmt.Sprintf("c.layout != %s", sqlLiteral(LayoutForeign))
	agg := [][2]string{
		{"c.database", "database"},
		{"c.table", "table"},
		{"count()", "n_columns"},
		{fmt.Sprintf("countIf(c.layout = %s)", sqlLiteral(LayoutPlain)), "n_plain_columns"},
		{fmt.Sprintf("countIf(c.layout = %s)", sqlLiteral(LayoutTagged)), "n_tagged_columns"},
		{fmt.Sprintf("countIf(c.layout = %s)", sqlLiteral(LayoutForeign)), "n_foreign_columns"},
		{"arraySort(groupUniqArrayIf(c.section, c.section != ''))", "sections"},
		{"uniqExactIf(c.section, c.section != '')", "n_sections"},
		{"anyIf(c.table_row_config, " + notForeign + ")", "table_row_config"},
		{"uniqExactIf(c.table_row_config, " + notForeign + ")", "n_table_row_configs"},
		{"arraySort(groupUniqArrayIf(c.streaming_group, " + notForeign + "))", "streaming_groups"},
		{"sum(c.data_compressed_bytes)", "data_compressed_bytes"},
		{"sum(c.data_uncompressed_bytes)", "data_uncompressed_bytes"},
	}
	inner := "SELECT\n" + selectList(agg) +
		"\nFROM " + target.Qualified(ViewColumns) + " AS c" +
		"\nGROUP BY c.database, c.table"

	shape := fmt.Sprintf("multiIf(a.n_columns = 0, %s, a.n_foreign_columns = a.n_columns, %s, a.n_foreign_columns = 0, %s, %s)",
		sqlLiteral(NameShapeEmpty), sqlLiteral(NameShapeForeign), sqlLiteral(NameShapeLeeway), sqlLiteral(NameShapeMixed))

	outer := make([][2]string, 0, len(TablesColumns()))
	outer = append(outer, [2]string{"t.database", "database"}, [2]string{"t.name", "name"})
	for _, c := range tablePassthroughColumns {
		outer = append(outer, [2]string{"t." + c, c})
	}
	outer = append(outer, [2]string{shape, "name_shape"})
	for _, c := range tableAggregateColumns {
		outer = append(outer, [2]string{"a." + c, c})
	}

	sql = "SELECT\n" + selectList(outer) +
		"\nFROM system.tables AS t\nLEFT JOIN\n(\n" + indent(inner, "    ") + "\n) AS a ON (t.database = a.database) AND (t.name = a.table)" +
		"\nWHERE " + notSystemDatabases("t.database") +
		"\nSETTINGS join_use_nulls = 0"
	return
}
