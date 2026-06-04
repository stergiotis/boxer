package dql

import _ "embed"

// helperUDFsSQL is the ClickHouse DDL that creates the leeway DQL
// read-back helper UDFs. See lw_dql_udfs.sql and EXPLANATION.md.
//
//go:embed lw_dql_udfs.sql
var helperUDFsSQL string

// HelperUDFsSQL returns the ClickHouse DDL that creates the leeway DQL
// jagged-array read-back helper UDFs: the LEEWAY_LU_* index-mapping
// family, LEEWAY_VALUE_BY_TAG_EQUAL (scalar value by membership),
// LEEWAY_UNFLATTEN, and LEEWAY_LIST_BY_TAG_EQUAL (array/set value by
// membership). Execute it once per database before running generated
// read-back queries; every statement is CREATE OR REPLACE, so re-running
// is safe.
func HelperUDFsSQL() string {
	return helperUDFsSQL
}
