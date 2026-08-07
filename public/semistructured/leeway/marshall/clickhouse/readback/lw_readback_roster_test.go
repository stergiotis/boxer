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

// TestRosterNamespace pins the family to the one leeway namespace (ADR-0162
// §SD2 as amended 2026-08-07). The panel's server probe asks a server for
// `LW\_%`; a family member outside that prefix would be invisible to it and
// would report as missing on every endpoint.
func TestRosterNamespace(t *testing.T) {
	for _, f := range readback.HelperFunctions() {
		require.Truef(t, strings.HasPrefix(f.Name, "LW_"), "%s is outside the LW_ namespace", f.Name)
	}
}
