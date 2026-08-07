package readback

import (
	_ "embed"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
)

// helperUDFsSQL is the ClickHouse DDL that creates the LW_LU_* helper
// family. See lw_readback_udfs.sql and EXPLANATION.md.
//
//go:embed lw_readback_udfs.sql
var helperUDFsSQL string

// HelperUDFsSQL returns the ClickHouse DDL that provisions the leeway DQL
// read-back helpers: the co/ragged function pack (ADR-0162) first — level-2
// unflattening is the pack's LW_RAGGED_NEST — then the LW_LU_*
// index-mapping family, LW_VALUE_BY_TAG_EQUAL (scalar value by
// membership) and LW_LIST_BY_TAG_EQUAL (array/set value by membership)
// layered on it. Execute it once per database before running generated
// read-back queries; every statement is CREATE OR REPLACE, so re-running is
// safe.
func HelperUDFsSQL() string {
	return strings.Join(chpack.Statements(), ";\n") + ";\n" + helperUDFsSQL
}
