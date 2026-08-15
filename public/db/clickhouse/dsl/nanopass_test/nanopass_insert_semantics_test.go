package nanopass_test

// ADR-0181 §SD8 M1 semantics: scopes under the INSERT wrapper are the SELECT
// source's with the target a structural sink; canonicalisation reaches the
// wrapper (identifier quoting, keyword case, the TABLE noise word dropped)
// and its output passes the terminal grammar2 proof; QualifyTables touches
// FROM-side tables only; ExposeSelectionConditions refuses loudly rather
// than appending columns the target could never match.

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/require"
)

func TestInsertWrapperScopesAreTheSource(t *testing.T) {
	pr, err := nanopass.Parse("INSERT INTO tgt (a) SELECT s.x FROM src AS s JOIN other AS o ON s.id = o.id WHERE s.x > 0")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "db")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	var names []string
	for i := range scopes[0].Tables {
		names = append(names, scopes[0].Tables[i].Table)
	}
	require.ElementsMatch(t, []string{"src", "other"}, names,
		"the source scope holds the FROM-side tables; the INSERT target is a sink, never a scope table")

	// A WITH inside the source rides selectStmt's withClause (the wrapper
	// has no query-level ctes), and the CTE registers in the same scope.
	pr, err = nanopass.Parse("INSERT INTO tgt WITH c AS (SELECT 1 AS x) SELECT x FROM c")
	require.NoError(t, err)
	scopes, err = nanopass.BuildScopes(pr, "db")
	require.NoError(t, err)
	require.NotEmpty(t, scopes)
}

func TestInsertWrapperCanonicalizes(t *testing.T) {
	chain := nanopass.Sequence("insertCanon", passes.StripComments, passes.CanonicalizeFull(100))
	out, err := chain.Run("insert into TABLE db.t (a, b) select x from src")
	require.NoError(t, err)
	require.Equal(t, `INSERT INTO "db"."t" ("a", "b") SELECT "x" FROM "src"`, out)

	// Idempotent on its own output, and the output is grammar2-canonical —
	// the terminal proof every chain ends on. Both would fail if the TABLE
	// noise word survived: grammar2's mirror has no TABLE alternative.
	again, err := chain.Run(out)
	require.NoError(t, err)
	require.Equal(t, out, again)
	_, err = nanopass.ValidateGrammar2.Run(out)
	require.NoError(t, err)
}

func TestInsertWrapperQualifyTouchesOnlyTheSource(t *testing.T) {
	out, err := passes.QualifyTables(`"anchor"`).Run("INSERT INTO tgt SELECT x FROM facts")
	require.NoError(t, err)
	require.Contains(t, out, `"anchor".facts`)
	require.Contains(t, out, "INSERT INTO tgt SELECT", "the target keeps its spelling — qualification is a scope-table affair")
}

func TestInsertWrapperRefusedBySelectionConditions(t *testing.T) {
	p := passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{})
	_, err := p.Run("INSERT INTO t SELECT a FROM src WHERE a > 0")
	require.ErrorContains(t, err, "ADR-0181 §SD8")
}
