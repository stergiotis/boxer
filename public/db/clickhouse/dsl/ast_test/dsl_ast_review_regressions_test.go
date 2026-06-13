package ast_test

// Regression tests for the 2026-06-12 hostile review of ast, astbuilder,
// and marshalling (round 3). Each test pins the corrected behaviour of a
// confirmed defect. The toAST/roundTripSemantics helpers implement the
// SEMANTIC round-trip oracle: parseability alone is blind to precedence
// regrouping and dropped clauses — both print valid SQL that means
// something else — so equivalence is checked structurally.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/astbuilder"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/require"
)

func toAST(t *testing.T, sql string) (ast.Query, string) {
	t.Helper()
	normalized, err := passes.CanonicalizeFull(16).Run(sql)
	require.NoError(t, err, "canonicalize %q", sql)
	pr, err := nanopass.ParseCanonical(normalized)
	require.NoError(t, err, "ParseCanonical %q", normalized)
	q, err := ast.ConvertCSTToAST(pr)
	require.NoError(t, err)
	return q, normalized
}

// Semantic oracle: AST → ToSQL → canonicalize → AST must be structurally
// identical (ToSQL must preserve meaning, not just parseability).
func roundTripSemantics(t *testing.T, sql string) (q1, q2 ast.Query, out string) {
	t.Helper()
	q1, _ = toAST(t, sql)
	out = q1.ToSQL()
	q2, _ = toAST(t, out)
	return
}

func TestRegressionAstPrecedenceRightAssoc(t *testing.T) {
	for _, sql := range []string{
		"SELECT a - (b - c) FROM t",
		"SELECT a / (b / c) FROM t",
		"SELECT -(a + b) FROM t",
		"SELECT NOT (a OR b) AND c FROM t",
		"SELECT (a BETWEEN b AND c) OR d FROM t",
		"SELECT a BETWEEN (b AND c) AND d FROM t",
		"SELECT (a = b) IS NULL FROM t",
		"SELECT NOT (a IS NULL) FROM t",
		"SELECT a - (b + c) * 2 FROM t",
	} {
		q1, q2, out := roundTripSemantics(t, sql)
		t.Logf("%q → %q", sql, out)
		// Structural oracle: comparing via ToSQL would be blind exactly
		// where ToSQL is broken (both groupings can print identically).
		if !reflect.DeepEqual(q1, q2) {
			t.Errorf("regression: semantics changed for %q\n out:  %s\n re:   %s", sql, out, q2.ToSQL())
		}
	}
}

func TestRegressionAstNestedWithDropped(t *testing.T) {
	q, norm := toAST(t, "SELECT x FROM (WITH c AS (SELECT 1 AS x) SELECT x FROM c)")
	out := q.ToSQL()
	t.Logf("normalized=%q\nout=%q", norm, out)
	if !strings.Contains(out, "WITH") {
		t.Errorf("regression: nested WITH dropped: %q", out)
	}
}

func TestRegressionAstLimitCommaSemantics(t *testing.T) {
	q, norm := toAST(t, "SELECT a FROM t LIMIT 5, 10")
	out := q.ToSQL()
	t.Logf("normalized=%q out=%q", norm, out)
	// ClickHouse: LIMIT m, n  ≡  LIMIT n OFFSET m
	if strings.Contains(out, "LIMIT 5 OFFSET 10") {
		t.Errorf("regression: comma-LIMIT flipped: %q (must be LIMIT 10 OFFSET 5)", out)
	}
}

func TestRegressionAstIdentifierQuotingLost(t *testing.T) {
	q, _ := toAST(t, `SELECT "my col", "select" FROM "my table"`)
	out := q.ToSQL()
	t.Logf("out=%q", out)
	if _, err := nanopass.Parse(out); err != nil {
		t.Errorf("regression: bare emission of quote-needing identifiers: %q: %v", out, err)
	}
}

func TestRegressionAstEscapedIdentifierCorruption(t *testing.T) {
	q, _ := toAST(t, "SELECT `a\"b` FROM t")
	out := q.ToSQL()
	t.Logf("out=%q", out)
	// The column is named a"b — whatever the spelling, it must round-trip.
	q2, _ := toAST(t, out)
	if q.ToSQL() != q2.ToSQL() {
		t.Errorf("regression: identifier with embedded quote corrupted: %q vs %q", q.ToSQL(), q2.ToSQL())
	}
}

func TestRegressionAstMarshalInt(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL(42) // plain int — documented as supported
	t.Logf("sql=%q err=%v", sql, err)
	if err != nil {
		t.Errorf("regression: int unsupported by MarshalGoValueToSQL: %v", err)
	}
}

func TestRegressionAstOctalDivergence(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("0777")
	require.NoError(t, err)
	v, err := lit.ToAny()
	require.NoError(t, err)
	t.Logf("0777 → %v (ClickHouse server: 777)", v)
	if v == uint64(511) {
		t.Errorf("regression: 0777 parsed as octal 511; ClickHouse treats it as decimal 777")
	}
}

func TestRegressionAstCompositeScavenging(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("1 + 2")
	t.Logf("lit=%+v err=%v", lit, err)
	if err == nil {
		v, _ := lit.ToAny()
		if v == uint64(1) {
			t.Errorf("regression: non-literal input silently scavenged to first literal: %v", v)
		}
	}
}

func TestRegressionAstBuilderOffsetPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("regression: Offset without Limit panics: %v", r)
		}
	}()
	out, err := astbuilder.Select(astbuilder.Col("a")).From("t").Offset(5).ToSQL()
	t.Logf("out=%q err=%v", out, err)
}

func TestRegressionAstBuilderSubDropsCTEs(t *testing.T) {
	inner := astbuilder.Select(astbuilder.Col("x")).From("c").
		With("c", astbuilder.Select(astbuilder.Lit(1).As("x")))
	out, err := astbuilder.Select(astbuilder.Star()).
		FromSubquery(inner, "sub").ToSQL()
	t.Logf("out=%q err=%v", out, err)
	if err == nil && !strings.Contains(out, "WITH") {
		t.Errorf("regression: subquery CTEs dropped by builder: %q", out)
	}
}

func TestRegressionAstSettingNameQuoting(t *testing.T) {
	out, err := passes.CanonicalizeFull(16).Run("SELECT a FROM t SETTINGS max_threads = 4")
	require.NoError(t, err)
	t.Logf("canonical=%q", out)
	if strings.Contains(out, `"max_threads"`) {
		t.Errorf("regression: canonical form quotes setting names (server compatibility risk): %q", out)
	}
}

// --- Round 5 (2026-06-13): findings from the AST round-trip fuzzer and the
// clickhouse-local server-truth harness. ---

// Fold negate-of-numeric-literal into the signed literal — `-(0)` and `-0`
// must converge to one AST shape (the numberLiteral rule owns the sign, so
// `-0` re-parses as a single literal).
func TestRegressionAstNegateLiteralFold(t *testing.T) {
	for _, sql := range []string{"SELECT -(0)", "SELECT -(5)", "SELECT -(-3)", "SELECT -(2.5)"} {
		q1, q2, out := roundTripSemantics(t, sql)
		if !reflect.DeepEqual(q1, q2) {
			t.Errorf("negate fold not stable for %q (out=%q)", sql, out)
		}
		require.NotContains(t, out, "--", "double-minus must never appear: %q", out)
	}
}

// EXTRACT lowers to the server's own unit functions, not extract(expr,'U')
// (which is the regex function — illegal on dates) and not nonexistent
// trimLeading/trimTrailing.
func TestRegressionAstSugarServerCanonical(t *testing.T) {
	out, err := passes.CanonicalizeSugar.Run("SELECT EXTRACT(DAY FROM d), EXTRACT(YEAR FROM d) FROM t")
	require.NoError(t, err)
	require.Contains(t, out, "toDayOfMonth(")
	require.Contains(t, out, "toYear(")
	require.NotContains(t, out, "extract(")

	out, err = passes.CanonicalizeSugar.Run("SELECT TRIM(LEADING ' ' FROM s), TRIM(TRAILING ' ' FROM s) FROM t")
	require.NoError(t, err)
	require.Contains(t, out, "trimLeft(")
	require.Contains(t, out, "trimRight(")
	require.NotContains(t, out, "trimLeading(")
	require.NotContains(t, out, "trimTrailing(")
}

// A scalar WITH item at the query level (`expr AS name`) must not be
// dropped — the previous converter skipped non-CTE WITH items, detaching
// every reference.
func TestRegressionAstQueryLevelScalarWith(t *testing.T) {
	q, _ := toAST(t, "WITH 0 AS z SELECT z")
	out := q.ToSQL()
	require.Contains(t, out, "WITH")
	require.Contains(t, out, "AS z")
	// And it round-trips structurally.
	_, q2, _ := roundTripSemantics(t, "WITH 0 AS z SELECT z")
	require.True(t, reflect.DeepEqual(q, q2))
}

// A keyword-shaped setting/CTE name must be emitted quoted (Grammar2 needs
// IDENTIFIER), while ordinary names stay bare.
func TestRegressionAstKeywordSettingKey(t *testing.T) {
	q, _ := toAST(t, `SELECT 1 SETTINGS "AS" = 1`)
	out := q.ToSQL()
	require.Contains(t, out, `"AS" = 1`)
	_, err := nanopass.ParseCanonical(mustCanon(t, out))
	require.NoError(t, err)
}

// Identifier emission must never fuse adjacent quoted names into one token
// (`"a""b"` is the escape form): `SELECT "x", "y"` stays two columns.
func TestRegressionAstNoIdentifierFusion(t *testing.T) {
	for _, sql := range []string{`SELECT ""A""`, `SELECT "x", "y"`, "SELECT `a`, `b`"} {
		q1, q2, out := roundTripSemantics(t, sql)
		require.True(t, reflect.DeepEqual(q1, q2), "fusion changed AST for %q (out=%q)", sql, out)
	}
}

// EscapeString output is always valid UTF-8: a value carrying a raw byte
// (decoded from \x80) round-trips through parseable SQL.
func TestRegressionAstEscapeUTF8(t *testing.T) {
	q1, q2, out := roundTripSemantics(t, `SELECT COLUMNS('\x80')`)
	require.True(t, reflect.DeepEqual(q1, q2), "out=%q", out)
}

func mustCanon(t *testing.T, sql string) string {
	t.Helper()
	out, err := passes.CanonicalizeFull(16).Run(sql)
	require.NoError(t, err)
	return out
}
