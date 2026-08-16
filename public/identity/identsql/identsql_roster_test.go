package identsql_test

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/identity/identsql"
)

var createRe = regexp.MustCompile(`CREATE OR REPLACE FUNCTION\s+(\w+)\s+AS\s+\(([^)]*)\)`)

// TestRosterMatchesDdl pins the declared roster to the DDL identsql actually
// emits: same names, same order, same parameters. A member added to
// UdfDdlStatements without a roster entry would be invisible to play's
// vocabulary panel; one added to the roster without DDL would be reported
// missing on every server (ADR-0174 §SD3).
func TestRosterMatchesDdl(t *testing.T) {
	stmts := identsql.UdfDdlStatements()
	roster := identsql.Functions()
	require.Len(t, stmts, len(roster), "roster size differs from the DDL statement count")

	for i, stmt := range stmts {
		m := createRe.FindStringSubmatch(stmt)
		require.NotNilf(t, m, "statement %d is not a CREATE OR REPLACE FUNCTION: %s", i, stmt)

		params := []string{}
		for p := range strings.SplitSeq(m[2], ",") {
			if p = strings.TrimSpace(p); p != "" {
				params = append(params, p)
			}
		}
		require.Equal(t, roster[i].Name, m[1], "roster entry %d", i)
		require.Equal(t, sqlvocab.ParamNames(roster[i].Params), params, "%s parameters", roster[i].Name)
		require.NotEmpty(t, roster[i].Doc, "%s has no doc line", roster[i].Name)
	}
}

// TestRosterIsExpandable pins the claim the roster's doc comment makes and
// which the panel repeats to a user: every member of this family is expanded
// client-side, so writing one works against an endpoint that carries none of
// the UDFs. A member the expander does not know would travel to the server
// and fail there.
func TestRosterIsExpandable(t *testing.T) {
	for _, f := range identsql.Functions() {
		call := f.Name + "(" + strings.Join(sqlvocab.ParamNames(f.Params), ", ") + ")"
		out, err := identsql.ExpandPass.Run("SELECT " + call)
		require.NoErrorf(t, err, "%s did not expand", f.Name)
		require.NotContainsf(t, out, f.Name, "%s survived expansion", f.Name)
	}
}

// TestRosterNamespace pins the family to the leeway namespace the rest of the
// vocabulary moved onto (ADR-0162 §SD2 as amended 2026-08-07). This family
// already had the shape; the guard is against a future member losing it.
func TestRosterNamespace(t *testing.T) {
	for _, f := range identsql.Functions() {
		require.Truef(t, strings.HasPrefix(f.Name, "LW_ID_"), "%s is outside the LW_ID_ family", f.Name)
		require.Equal(t, nanopass.NormalizeCallName(f.Name), strings.ToLower(f.Name),
			"%s does not normalise to its own lowercase spelling", f.Name)
	}
}

// TestRosterDeclaresEveryDomain is ADR-0190 §SD4's floor for this roster: a
// parameter whose domain is unsaid is refused at registration, so the check is
// simply that the whole roster registers.
func TestRosterDeclaresEveryDomain(t *testing.T) {
	r := sqlvocab.NewRegistry()
	for _, f := range identsql.Functions() {
		require.NoErrorf(t, r.Register(sqlvocab.Function{
			Name: f.Name, Params: f.Params, Doc: f.Doc, Where: sqlvocab.WhereClient,
		}), "%s", f.Name)
	}
	require.NotZero(t, r.Len())
}
