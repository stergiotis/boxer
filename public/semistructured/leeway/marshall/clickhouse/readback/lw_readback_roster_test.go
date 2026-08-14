package readback_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
)

// createRe matches one CREATE OR REPLACE FUNCTION header and captures the
// name and the raw parameter list. It is used only by the test — the run-time
// roster is declared, not parsed (ADR-0174 §SD3).
var createRe = regexp.MustCompile(`CREATE OR REPLACE FUNCTION\s+(\w+)\s+AS\s+\(([^)]*)\)`)

// TestRosterMatchesSQL is the pin between the declared roster and the SQL
// that actually provisions the family: same names, same order, same
// parameters. Without it the roster is a comment that drifts, and the panel
// reading it would report a function as missing on a server that has it, or
// list one that was never created.
//
// Scoped to the embedded family file by subtracting chpack's roster from
// HelperUDFsSQL(), rather than reading the .sql through a second embed: what
// this must pin is what HelperUDFsSQL emits, which is the pair.
func TestRosterMatchesSQL(t *testing.T) {
	sql := readback.HelperUDFsSQL()

	packNames := make(map[string]struct{}, len(chpack.Functions()))
	for _, f := range chpack.Functions() {
		packNames[f.Name] = struct{}{}
	}

	type parsed struct {
		name   string
		params []string
	}
	var got []parsed
	for _, m := range createRe.FindAllStringSubmatch(sql, -1) {
		if _, isPack := packNames[m[1]]; isPack {
			continue
		}
		params := []string{}
		for p := range strings.SplitSeq(m[2], ",") {
			if p = strings.TrimSpace(p); p != "" {
				params = append(params, p)
			}
		}
		got = append(got, parsed{name: m[1], params: params})
	}

	roster := readback.HelperFunctions()
	require.Len(t, got, len(roster), "roster size differs from the statements HelperUDFsSQL emits")
	for i, want := range roster {
		require.Equal(t, want.Name, got[i].name, "roster entry %d", i)
		require.Equal(t, want.Params, got[i].params, "%s parameters", want.Name)
		require.NotEmpty(t, want.Doc, "%s has no doc line", want.Name)
	}
}

// TestFamilyStatementsMatchRoster pins the lexical split FamilyStatements
// does — strip `--` comments, cut on `;` — against the declared roster.
//
// The split assumes no body contains a `;` inside a string literal. That is
// true of the file today and this is what keeps it true: a body that broke
// the assumption would produce a statement count or an order that no longer
// matches the roster, failing here rather than installing half a function.
func TestFamilyStatementsMatchRoster(t *testing.T) {
	stmts := readback.FamilyStatements()
	roster := readback.HelperFunctions()
	require.Len(t, stmts, len(roster), "one statement per declared function")

	for i, want := range roster {
		m := createRe.FindStringSubmatch(stmts[i])
		require.NotNilf(t, m, "statement %d is not a CREATE OR REPLACE FUNCTION: %s", i, stmts[i])
		require.Equal(t, want.Name, m[1], "statement %d", i)
		require.NotContains(t, stmts[i], ";", "statements are cut at the separator")
		require.NotContains(t, stmts[i], "--", "comments are stripped")
	}

	// The pack is HelperUDFsSQL's business, not this family's: the surface
	// installer provisions the pack itself and must not receive it twice.
	for _, f := range chpack.Functions() {
		for _, stmt := range stmts {
			require.NotContainsf(t, stmt, "FUNCTION "+f.Name+" ", "%s leaked into the family statements", f.Name)
		}
	}
}

// TestRosterNamespace pins the family to the one leeway namespace (ADR-0162
// §SD2 as amended 2026-08-07). The panel's server probe asks a server for
// `LW\_%`; a family member outside that prefix would be invisible to it and
// would report as missing on every endpoint.
func TestRosterNamespace(t *testing.T) {
	for _, f := range readback.HelperFunctions() {
		require.Truef(t, strings.HasPrefix(f.Name, "LW_"), "%s is outside the LW_ namespace", f.Name)
	}
}
