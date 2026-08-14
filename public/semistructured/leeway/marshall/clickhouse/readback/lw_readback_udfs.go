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

// FamilyStatements returns this family's CREATE statements alone, in
// declaration order, without the pack HelperUDFsSQL prepends.
//
// It exists for the surface installer (ADR-0171 §SD2), which executes one
// statement per round trip — ClickHouse's HTTP interface takes one query per
// request — and provisions the pack itself, so it must not receive it twice.
// Callers provisioning by hand want HelperUDFsSQL, which is the whole script
// and stays the documented path.
//
// The split is lexical: strip `--` line comments, cut on `;`. That is safe
// for this file and only this file — no body contains a string literal, and
// TestFamilyStatementsMatchRoster pins the result against HelperFunctions,
// so a future body that breaks the assumption fails the build rather than
// installing half a function.
func FamilyStatements() (stmts []string) {
	var b strings.Builder
	b.Grow(len(helperUDFsSQL))
	for line := range strings.SplitSeq(helperUDFsSQL, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	stmts = make([]string, 0, 8)
	for stmt := range strings.SplitSeq(b.String(), ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return
}
