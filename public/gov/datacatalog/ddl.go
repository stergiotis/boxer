package datacatalog

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
)

// The catalog's ClickHouse coordinates.
const (
	// DatabaseName is where the catalog lives. It is factsschema's constant,
	// not a second spelling of the same string: the catalog is a boxer table
	// like the facts table, and one of them moving without the other would be
	// a silent split.
	DatabaseName = factsschema.DatabaseName

	// TableCatalog is the inventory: every discovered table, both kinds. It
	// exists so that "matches no known shape" and "looks leeway but does not
	// parse" are rows a reader can select, not absences they have to infer.
	TableCatalog = "tables_catalog"
	// TableLeeway is the restoration payload, one row per leeway table.
	TableLeeway = "tables_leeway"
	// TableCompatibility is the pair matrix, one row per unordered pair of
	// leeway tables — disjoint pairs included.
	TableCompatibility = "tables_leeway_compatibility"
	// TableOpaqueShapes is one row per satisfied (opaque table, panel shape).
	TableOpaqueShapes = "tables_opaque_shapes"
)

// AllTables lists the catalog's own tables in the order a rebuild replaces
// them.
var AllTables = []string{TableCatalog, TableLeeway, TableCompatibility, TableOpaqueShapes}

// TargetDatabase is where a run writes its four tables. The empty value means
// [DatabaseName]: the catalog belongs in boxer, and only a staging rebuild or a
// test that must not disturb the real one has reason to say otherwise.
//
// What a run *reads* is not configurable — the whole server outside the system
// databases, per ADR-0170 §SD1. Only the destination moves.
type TargetDatabase string

// Name resolves the target, applying the default.
func (inst TargetDatabase) Name() (name string) {
	if inst == "" {
		return DatabaseName
	}
	return string(inst)
}

// Qualified renders `database.table` for one of the catalog tables.
func (inst TargetDatabase) Qualified(table string) (name string) {
	return inst.Name() + "." + table
}

// Qualified renders `database.table` in the default target — what a reader
// writing a book chapter or a doc means by the table's name.
func Qualified(table string) (name string) {
	return TargetDatabase("").Qualified(table)
}

// The Enum8 vocabularies are pinned to the Go enums' numeric values —
// KindOpaque is 0 and KindLeeway is 1, TableRelationDisjoint is 0 through
// TableRelationEqual is 4 — so the number ClickHouse stores and the number the
// analysis computed are the same number. A reordering of either side without
// the other turns the round trip silently wrong, which is why they are written
// out here rather than left to insertion order.
const (
	kindEnumDdl     = `Enum8('opaque' = 0, 'leeway' = 1)`
	relationEnumDdl = `Enum8('disjoint' = 0, 'overlap' = 1, 'subset' = 2, 'superset' = 3, 'equal' = 4)`
)

// DDL returns the statements one refresh applies, in order: the database, then
// each table as a `CREATE OR REPLACE TABLE`.
//
// Replace, not truncate-and-insert: the source of truth is the physical schema
// itself, and a full rebuild states that (ADR-0170 §SD2). The cost is that a
// reader mid-rebuild can observe a fresh-but-empty table; run_id and
// discovered_at are on every row so that staleness, at least, is visible.
//
// MergeTree with no partitioning: at a hundred tables and a few thousand pair
// rows the whole catalog is smaller than one granule's worth of most tables it
// describes.
func (inst TargetDatabase) DDL() (statements []string) {
	return []string{
		`CREATE DATABASE IF NOT EXISTS ` + inst.Name(),

		`CREATE OR REPLACE TABLE ` + inst.Qualified(TableCatalog) + ` (
	database String,
	name String,
	engine String,
	kind ` + kindEnumDdl + `,
	n_columns UInt32,
	normalized_schema String,
	classify_detail String,
	run_id String,
	discovered_at DateTime
) ENGINE = MergeTree ORDER BY (database, name)`,

		`CREATE OR REPLACE TABLE ` + inst.Qualified(TableLeeway) + ` (
	database String,
	name String,
	table_row_config String,
	schema_hash UInt64,
	n_attrs UInt32,
	attr_keys Array(String),
	desc_json String,
	run_id String,
	discovered_at DateTime
) ENGINE = MergeTree ORDER BY (database, name)`,

		`CREATE OR REPLACE TABLE ` + inst.Qualified(TableCompatibility) + ` (
	database_a String,
	name_a String,
	database_b String,
	name_b String,
	relation ` + relationEnumDdl + `,
	shape_id UInt64,
	n_common UInt32,
	jaccard Float32,
	run_id String,
	discovered_at DateTime
) ENGINE = MergeTree ORDER BY (database_a, name_a, database_b, name_b)`,

		`CREATE OR REPLACE TABLE ` + inst.Qualified(TableOpaqueShapes) + ` (
	database String,
	name String,
	shape String,
	run_id String,
	discovered_at DateTime
) ENGINE = MergeTree ORDER BY (shape, database, name)`,
	}
}

// DDLText joins [TargetDatabase.DDL] into one script, each statement
// terminated — what `--dry-run` prints and what a reader pastes into a client.
func (inst TargetDatabase) DDLText() (script string) {
	var b strings.Builder
	for _, s := range inst.DDL() {
		b.WriteString(s)
		b.WriteString(";\n\n")
	}
	return b.String()
}

// DDL and DDLText in the default target, for a caller with nothing to say about
// where the catalog lives.
func DDL() (statements []string) { return TargetDatabase("").DDL() }
func DDLText() (script string)   { return TargetDatabase("").DDLText() }
