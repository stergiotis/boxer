package factsschema

// The physical layout boxer.facts is created with (ADR-0397). It is the one
// source both the table's creator (factsstore/chstore, through the
// BOXER_FACTS_ENGINE setting whose default it is) and the DDL goldens of the
// facts-bound stores (storegen) take the engine clause from, so the goldens
// state what a new table is.

// TsColumn is the physical name of the facts envelope's timestamp, the column
// the layout partitions and sorts by. A change of its aspects renames it; the
// ddl package's tests fail until this follows.
const TsColumn = "ts:ts:z64:47::0:"

// DefaultEngineClause is the engine clause of a newly created facts table:
// time-sorted, one partition per calendar month of the row's timestamp.
//
// The sort serves the reads that bound or order by ts (the log rows, the app
// trail, the run events); the month partition lets those reads skip whole
// months, makes retention a DROP PARTITION per month rather than a mutation,
// and keeps a year at twelve partitions. Live writes land in the current
// month, so they produce the parts they did unpartitioned. An insert block
// spanning more months than the server's max_partitions_per_insert_block
// (100 by default) is refused; an import of older rows is sent in blocks of
// fewer months. No TTL is imposed: deleting rows is the deployment's
// decision. ADR-0397 weighs the alternatives.
//
// The identifier is double-quoted, as the generated column list quotes it;
// that also keeps backticks, which a shell expands, out of a value someone
// copies into BOXER_FACTS_ENGINE.
const DefaultEngineClause = `MergeTree() PARTITION BY toYYYYMM("` + TsColumn + `") ORDER BY "` + TsColumn + `"`

// MigrationRecipe is the recipe that copies a facts table created with
// another layout into this one.
const MigrationRecipe = "doc/migration/2026-11-facts-layout.md"
