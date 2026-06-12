package nanopass_test

// Regression tests for the 2026-06-12 hostile-review findings. Each test
// pins the corrected behaviour of a confirmed defect; the comment names the
// finding. See doc/changelog for the review itself.

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C1: a CTE referencing an earlier CTE must not have the reference
// qualified as a physical table.
func TestRegressionChainedCTENotQualified(t *testing.T) {
	out, err := passes.QualifyTables("db").Run(
		"WITH a AS (SELECT 1 AS x), b AS (SELECT x FROM a) SELECT x FROM b")
	require.NoError(t, err)
	assert.NotContains(t, out, "db.a")
	assert.NotContains(t, out, "db.b")
}

// C2: members of a parenthesised union must not be dropped from scope
// analysis — every branch's table gets qualified.
func TestRegressionParenUnionAllBranchesQualified(t *testing.T) {
	out, err := passes.QualifyTables("db").Run(
		"(SELECT x FROM t1 UNION ALL SELECT x FROM t2) UNION ALL SELECT x FROM t3")
	require.NoError(t, err)
	assert.Contains(t, out, "db.t1")
	assert.Contains(t, out, "db.t2")
	assert.Contains(t, out, "db.t3")
}

// C2: BuildScopes flattens nested parenthesised unions into the member list.
func TestRegressionParenUnionScopeCount(t *testing.T) {
	pr, err := nanopass.Parse("(SELECT 1 UNION ALL SELECT 2) UNION ALL SELECT 3")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 3)
	for _, s := range scopes {
		assert.Len(t, s.UnionMembers, 3)
	}
}

// C3: a WITH clause attached to the selectStmt (inside a subquery) defines
// CTEs — the reference must not be database-qualified.
func TestRegressionSubqueryLevelWithNotQualified(t *testing.T) {
	out, err := passes.QualifyTables("db").Run(
		"SELECT x FROM (WITH c AS (SELECT 1 AS x) SELECT x FROM c)")
	require.NoError(t, err)
	assert.NotContains(t, out, "db.c")
}

// C3: same for a WITH clause in a non-first UNION branch.
func TestRegressionUnionBranchWithNotQualified(t *testing.T) {
	out, err := passes.QualifyTables("db").Run(
		"SELECT 1 AS x UNION ALL WITH c AS (SELECT 2 AS x) SELECT x FROM c")
	require.NoError(t, err)
	assert.NotContains(t, out, "db.c")
}

// H2: scalar subqueries in projection position (ColumnsExprSubquery) are
// part of scope analysis.
func TestRegressionProjectionScalarSubqueryQualified(t *testing.T) {
	out, err := passes.QualifyTables("db").Run(
		"SELECT (SELECT max(v) FROM other) FROM t")
	require.NoError(t, err)
	assert.Contains(t, out, "db.other")
	assert.Contains(t, out, "db.t")
}

// H3: the bare alias form (no AS keyword) is captured.
func TestRegressionBareAliasResolvable(t *testing.T) {
	pr, err := nanopass.Parse("SELECT o.a FROM orders o")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	src, found := scopes[0].ResolveAlias("o")
	require.True(t, found)
	assert.Equal(t, "orders", src.Table)
	// The alias hides the table name.
	_, found = scopes[0].ResolveAlias("orders")
	assert.False(t, found)
}

// H1: lexically invalid input is rejected instead of silently dropping the
// offending characters from rewritten output.
func TestRegressionLexerErrorRejected(t *testing.T) {
	_, err := nanopass.Parse("SELECT \x01 1")
	require.Error(t, err)
	_, err = passes.StripComments.Run("SELECT \x01 1")
	require.Error(t, err)
}

// M11: parse errors carry line:column positions.
func TestRegressionParseErrorHasPosition(t *testing.T) {
	_, err := nanopass.Parse("SELECT FROM WHERE")
	require.Error(t, err)
	assert.Regexp(t, `\d+:\d+`, err.Error())
}

// H4: double negation must not produce a "--" prefix (SQL line comment).
func TestRegressionMacroDoubleNegation(t *testing.T) {
	var got []nanopass.LiteralArg
	me := nanopass.NewMacroExpander()
	me.Register("m", func(args []nanopass.LiteralArg) (string, error) {
		got = append([]nanopass.LiteralArg{}, args...)
		return args[0].Value, nil
	})
	out, err := me.Pass().Run("SELECT m(-(-5))")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "5", got[0].Value)
	assert.Equal(t, "SELECT 5", out)
}

// H5: nested macros expand fully in one Run (the pass declares
// NeedsFixedPoint).
func TestRegressionMacroNestedFullyExpands(t *testing.T) {
	me := nanopass.NewMacroExpander()
	me.Register("two", func(args []nanopass.LiteralArg) (string, error) { return "2", nil })
	me.Register("double", func(args []nanopass.LiteralArg) (string, error) {
		return "(" + args[0].Value + " * 2)", nil
	})
	out, err := me.Pass().Run("SELECT double(two())")
	require.NoError(t, err)
	assert.Equal(t, "SELECT (2 * 2)", out)
}

// L6: boolean keyword arguments classify as LiteralTypeBool.
func TestRegressionMacroBoolArg(t *testing.T) {
	var got []nanopass.LiteralArg
	me := nanopass.NewMacroExpander()
	me.Register("m", func(args []nanopass.LiteralArg) (string, error) {
		got = append([]nanopass.LiteralArg{}, args...)
		return "1", nil
	})
	_, err := me.Pass().Run("SELECT m(true)")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, nanopass.LiteralTypeBool, got[0].Type)
}

// L8: macro registry matching is quoting-insensitive, like the
// FunctionEvaluator's.
func TestRegressionMacroQuotedNameMatches(t *testing.T) {
	me := nanopass.NewMacroExpander()
	me.Register("myMacro", func(args []nanopass.LiteralArg) (string, error) { return "42", nil })
	out, err := me.Pass().Run(`SELECT "myMacro"()`)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 42", out)
}

// M1: the discard marker inside a string literal must not turn the pass
// into the identity function.
func TestRegressionMarkerInStringLiteralIgnored(t *testing.T) {
	sql := "SELECT '" + nanopass.PassDiscardOutputMarker + "' /* strip me */ FROM t"
	out, err := passes.StripComments.Run(sql)
	require.NoError(t, err)
	assert.NotContains(t, out, "strip me")
	assert.Contains(t, out, nanopass.PassDiscardOutputMarker) // the literal survives
}

// M2: discard drops the body rewrite only; env mutations persist, so
// Run(p) == Run(Sequence(p)).
func TestRegressionDiscardEnvSymmetry(t *testing.T) {
	analytical := nanopass.Pass{
		Name: "analytical",
		Apply: func(e *env.Environment, body string) (string, error) {
			e.SessionSettings["max_threads"] = env.Setting{Name: "max_threads", Raw: "8"}
			return body + " " + nanopass.PassDiscardOutputMarker, nil
		},
	}
	direct, err := analytical.Run("SELECT 1")
	require.NoError(t, err)
	seq, err := nanopass.Sequence("s", analytical).Run("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, direct, seq)
	assert.Contains(t, direct, "SET max_threads = 8;")
}

// M3: CTE definitions shared between union members are visited exactly once
// by FlattenScopes.
func TestRegressionFlattenScopesDedupes(t *testing.T) {
	pr, err := nanopass.Parse(
		"WITH c AS (SELECT 1 AS x) SELECT x FROM c UNION ALL SELECT 2 AS x")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	seen := map[*nanopass.SelectScope]int{}
	for _, s := range nanopass.FlattenScopes(scopes) {
		seen[s]++
	}
	for s, n := range seen {
		assert.Equal(t, 1, n, "scope %p visited %d times", s, n)
	}
}

// H6: conflicting token edits fail the pass with an error instead of
// crashing the process (ANTLR panics inside GetText; the Pass boundary
// recovers).
func TestRegressionOverlapPanicRecoveredToError(t *testing.T) {
	p := nanopass.LiftBodyPass("conflicting",
		func(sql string) (string, error) {
			pr, err := nanopass.Parse(sql)
			if err != nil {
				return "", err
			}
			rw := nanopass.NewRewriter(pr)
			rw.ReplaceDefault(1, 4, "X")
			rw.ReplaceDefault(3, 6, "Y") // partial overlap → GetText panics
			return nanopass.GetText(rw), nil
		}, nanopass.PassProperties{})
	_, err := p.Run("SELECT a + b + c FROM t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panicked")
}

// M4: Validating delegates the wrapped pass's fixpoint and clears
// NeedsFixedPoint — no nested double loop (minimal apply count).
func TestRegressionValidatingNoDoubleFixpoint(t *testing.T) {
	applies := 0
	p := nanopass.Pass{
		Name: "growOnce",
		Apply: func(e *env.Environment, body string) (string, error) {
			applies++
			if body == "SELECT 1" {
				return "SELECT 1, 2", nil
			}
			return body, nil
		},
		Properties: nanopass.PassProperties{NeedsFixedPoint: true},
	}
	wrapped := nanopass.Validating(nanopass.GrammarG1, p)
	assert.False(t, wrapped.Properties.NeedsFixedPoint)
	out, err := nanopass.Sequence("s", wrapped).Run("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1, 2", out)
	// change + convergence check — nothing more.
	assert.Equal(t, 2, applies)
}

// M4/L1: FixedPoint preserves properties, declares Idempotent, and clears
// NeedsFixedPoint.
func TestRegressionFixedPointProperties(t *testing.T) {
	p := nanopass.Pass{
		Name:       "p",
		Apply:      func(_ *env.Environment, b string) (string, error) { return b, nil },
		Properties: nanopass.PassProperties{NeedsFixedPoint: true, Reads: nanopass.RegionBody},
	}
	fp := nanopass.FixedPoint(p, 4)
	assert.True(t, fp.Properties.Idempotent)
	assert.False(t, fp.Properties.NeedsFixedPoint)
	assert.Equal(t, nanopass.RegionBody, fp.Properties.Reads)
}

// L2: an unknown grammar selector fails validation loudly.
func TestRegressionUnknownGrammarErrors(t *testing.T) {
	p := nanopass.LiftBodyPass("id", func(s string) (string, error) { return s, nil }, nanopass.PassProperties{})
	_, err := nanopass.Validating(nanopass.Grammar(7), p).Run("SELECT 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown grammar")
}

// M10: quoted and bare spellings of the same name resolve to the same CTE.
func TestRegressionQuotedCTEReferenceNotQualified(t *testing.T) {
	out, err := passes.QualifyTables("db").Run(
		`WITH x AS (SELECT 1 AS a) SELECT a FROM "x"`)
	require.NoError(t, err)
	assert.NotContains(t, out, `db."x"`)
	assert.NotContains(t, out, "db.x")
}

// M12: table functions register as sources — the alias resolves, and
// qualification leaves the function alone.
func TestRegressionTableFunctionAlias(t *testing.T) {
	pr, err := nanopass.Parse("SELECT n.number FROM numbers(10) AS n")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	src, found := scopes[0].ResolveAlias("n")
	require.True(t, found)
	assert.True(t, src.IsFunction)
	assert.Equal(t, "numbers", src.Table)

	out, err := passes.QualifyTables("db").Run("SELECT n.number FROM numbers(10) AS n")
	require.NoError(t, err)
	assert.NotContains(t, out, "db.numbers")
}

// M5: SourceRange is a half-open byte range — correct slicing on non-ASCII
// input, empty zero value.
func TestRegressionSourceRangeBytesNonASCII(t *testing.T) {
	assert.True(t, nanopass.SourceRange{}.Empty())

	var observed []nanopass.Observation
	fe := passes.NewFunctionEvaluator()
	fe.Register("marked", func(args []any) (any, error) { return int64(1), nil }, true)
	fe.OnObservation(func(obs nanopass.Observation) { observed = append(observed, obs) })

	body := "SELECT 'héllø wörld', marked(1)"
	_, err := fe.Pass().Run(body)
	require.NoError(t, err)
	require.NotEmpty(t, observed)
	src := observed[0].Src
	require.False(t, src.Empty())
	assert.Equal(t, "marked(1)", body[src.Start:src.End])
}

// M6: with an observation hook installed, nested registered evaluators run
// exactly once per site.
func TestRegressionEvaluatorSingleInvoke(t *testing.T) {
	calls := 0
	fe := passes.NewFunctionEvaluator()
	fe.Register("inner", func(args []any) (any, error) {
		calls++
		return int64(7), nil
	}, true)
	fe.Register("outer", func(args []any) (any, error) { return args[0], nil }, true)
	fe.OnObservation(func(obs nanopass.Observation) {})
	out, err := fe.Pass().Run("SELECT outer(inner(1))")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 7", out)
	assert.Equal(t, 1, calls)
}

// M8: parenthesised negated literals are evaluable (negateValue handles
// TypedLiteral).
func TestRegressionEvaluatorParenNegate(t *testing.T) {
	fe := passes.NewFunctionEvaluator()
	fe.Register("idf", func(args []any) (any, error) { return args[0], nil }, true)
	out, err := fe.Pass().Run("SELECT idf(-(5))")
	require.NoError(t, err)
	assert.Equal(t, "SELECT -5", out)
}

// Identifier codec: decode/encode round-trips across spellings.
func TestRegressionIdentifierCodec(t *testing.T) {
	cases := map[string]string{
		"bare":      "bare",
		"`bt`":      "bt",
		"`b``t`":    "b`t",
		`"dq"`:      "dq",
		`"d""q"`:    `d"q`,
		"`a\\`b`":   "a`b",
		`"a\"b"`:    `a"b`,
		"`a\\\\b`":  `a\b`,
		`"a\\b"`:    `a\b`,
		"`a\"b`":    `a"b`,
		"`it's`":    "it's",
		`"select"`:  "select",
		"`mixed\"`": `mixed"`,
	}
	for spelling, want := range cases {
		assert.Equal(t, want, nanopass.DecodeIdentifier(spelling), "decode %q", spelling)
		reenc := nanopass.QuoteIdentifier(want)
		assert.Equal(t, want, nanopass.DecodeIdentifier(reenc), "round-trip %q", want)
	}
}
