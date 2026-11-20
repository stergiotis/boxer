// Package ddl is the target-independent half of leeway's DDL stage: the
// naming convention that turns a column's leeway coordinates into a
// physical column name, and the driver that walks an
// IntermediateTableRepresentation and asks a target's code generator for
// each column.
//
// [NewHumanReadableNamingConvention] is the naming convention the tree
// uses; its separator (":" throughout boxer) becomes part of every physical
// name, so a reader and a writer of one table must agree on it.
// [NewGeneratorDriver] returns the driver whose GenerateColumnsCode emits
// the column list. A complete CREATE TABLE for ClickHouse, with ORDER BY and
// indexes addressed by leeway coordinates rather than by physical name, is
// ComposeCreateTable in the clickhouse subpackage; see its example.
package ddl
