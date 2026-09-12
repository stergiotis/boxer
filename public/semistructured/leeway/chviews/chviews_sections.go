package chviews

import (
	"fmt"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// sectionsColumns is the (table, section)-grain contract.
var sectionsColumns = []string{
	"database",
	"table",
	"section",
	"layout",
	"n_columns",
	"n_value_columns",
	"value_columns",
	"roles",
	"lane_kinds",
	"canonical_types",
	"use_aspects",
	"streaming_group",
	"n_streaming_groups",
	"co_section_group",
	"n_co_section_groups",
	"table_row_config",
	"data_compressed_bytes",
	"data_uncompressed_bytes",
}

// SectionsColumns returns the view's result columns in order.
func SectionsColumns() (names []string) {
	names = make([]string, len(sectionsColumns))
	copy(names, sectionsColumns)
	return
}

// sectionsBody groups the decode by section.
//
// Streaming group and co-section group are section properties by
// construction, so the value is reported beside its distinct count rather
// than through a bare any(): a section whose columns disagree is a table
// somebody wrote by hand, and it should read as one row with a count of two,
// not as an arbitrary pick (ADR-0226 §SD3).
//
// Source columns are qualified. An aggregate aliased to the name of the
// column it consumes is a cyclic alias to the analyzer, and use_aspects is
// exactly that shape.
func sectionsBody(target TargetDatabase) (sql string) {
	value := sqlLiteral(lwsql.LaneKindValue.String())
	pairs := [][2]string{
		{"c.database", "database"},
		{"c.table", "table"},
		{"c.section", "section"},
		{"any(c.layout)", "layout"},
		{"count()", "n_columns"},
		{fmt.Sprintf("countIf(c.lane_kind = %s)", value), "n_value_columns"},
		{fmt.Sprintf("arraySort(groupUniqArrayIf(c.leeway_column, c.lane_kind = %s))", value), "value_columns"},
		{"arraySort(groupUniqArrayIf(c.role, c.role != ''))", "roles"},
		{"arraySort(groupUniqArray(c.lane_kind))", "lane_kinds"},
		{fmt.Sprintf("arraySort(groupUniqArrayIf(c.canonical_type, c.lane_kind = %s))", value), "canonical_types"},
		{"arraySort(groupUniqArrayArray(c.use_aspects))", "use_aspects"},
		{"any(c.streaming_group)", "streaming_group"},
		{"uniqExact(c.streaming_group)", "n_streaming_groups"},
		{"any(c.co_section_group)", "co_section_group"},
		{"uniqExact(c.co_section_group)", "n_co_section_groups"},
		{"any(c.table_row_config)", "table_row_config"},
		{"sum(c.data_compressed_bytes)", "data_compressed_bytes"},
		{"sum(c.data_uncompressed_bytes)", "data_uncompressed_bytes"},
	}
	sql = "SELECT\n" + selectList(pairs) +
		"\nFROM " + target.Qualified(ViewColumns) + " AS c" +
		"\nWHERE c.section != ''" +
		"\nGROUP BY c.database, c.table, c.section"
	return
}
