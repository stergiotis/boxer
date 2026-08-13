package constructsql

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stretchr/testify/require"
)

func expandOne(t *testing.T, sql string) string {
	t.Helper()
	out, err := ExpandPass.Run(sql)
	require.NoError(t, err)
	return out
}

func expandErr(t *testing.T, sql string) error {
	t.Helper()
	_, err := ExpandPass.Run(sql)
	require.Error(t, err)
	return err
}

func TestExpand_PlainGolden(t *testing.T) {
	got := expandOne(t, "SELECT LW_PLAIN(sum(x), 'total-revenue', 'u64', 'item:oq') FROM t")
	require.Equal(t, `SELECT sum(x) AS "oq:total-revenue:u64:::0:" FROM t`, got)
}

func TestExpand_TaggedGolden(t *testing.T) {
	got := expandOne(t, "SELECT LW_TV(v, 'mysec', 'myCol', 's') FROM t")
	require.Equal(t, `SELECT v AS "tv:mysec:my-col:val:s::::0::" FROM t`, got)
}

func TestExpand_MembershipAndSupport(t *testing.T) {
	c, err := lwsql.NewComposer(lwsql.DefaultTableSegments())
	require.NoError(t, err)
	memb, err := c.MembershipColumn("mysec", "low-card-ref")
	require.NoError(t, err)
	card, err := c.SupportColumn("mysec", "lrcard")
	require.NoError(t, err)

	got := expandOne(t, "SELECT LW_TV_MEMB(m, 'mysec', 'low-card-ref'), LW_TV_SUPPORT(k, 'mysec', 'lrcard') FROM t")
	require.Equal(t, `SELECT m AS "`+memb+`", k AS "`+card+`" FROM t`, got)
}

func TestExpand_CaseAndSpacingInsensitive(t *testing.T) {
	got := expandOne(t, "SELECT lw_plain( sum(x) , 'a', 'u64', 'item:oq' ) FROM t")
	require.Equal(t, `SELECT sum(x) AS "oq:a:u64:::0:" FROM t`, got)
}

func TestExpand_TriviaInExpressionSurvives(t *testing.T) {
	got := expandOne(t, "SELECT LW_PLAIN(sum(x) /* keep me */ + 1, 'a', 'u64', 'item:oq') FROM t")
	require.Contains(t, got, "/* keep me */")
}

func TestExpand_NoMarkerIsByteIdentical(t *testing.T) {
	in := "SELECT a, b FROM t WHERE lower(c) = 'lw'"
	out, err := ExpandPass.Run(in)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestExpand_ExpandedOutputReparses(t *testing.T) {
	got := expandOne(t, "SELECT LW_TV(v, 'mysec', 'c', 'u64h', 'enc:delta-encoding'), LW_TV_MEMB(m, 'mysec', 'low-card-ref') FROM t")
	_, err := nanopass.Parse(got)
	require.NoError(t, err)
}

func TestExpand_PositionRules(t *testing.T) {
	err := expandErr(t, "SELECT a FROM t WHERE LW_PLAIN(x, 'a', 'u64', 'item:oq') = 1")
	require.ErrorContains(t, err, "whole projection item")

	err = expandErr(t, "SELECT f(LW_PLAIN(x, 'a', 'u64', 'item:oq')) FROM t")
	require.ErrorContains(t, err, "whole projection item")

	err = expandErr(t, "SELECT LW_PLAIN(x, 'a', 'u64', 'item:oq') AS y FROM t")
	require.ErrorContains(t, err, "mints its own alias")

	// A constructor nested in another constructor's expression argument.
	err = expandErr(t, "SELECT LW_PLAIN(LW_TV(v, 's', 'c', 'u64'), 'a', 'u64', 'item:oq') FROM t")
	require.ErrorContains(t, err, "whole projection item")
}

func TestExpand_SubqueryProjectionItemIsLegal(t *testing.T) {
	got := expandOne(t, "SELECT * FROM (SELECT LW_PLAIN(x, 'a', 'u64', 'item:oq') FROM t)")
	require.Contains(t, got, `x AS "oq:a:u64:::0:"`)
}

func TestExpand_SpecArgRejections(t *testing.T) {
	err := expandErr(t, "SELECT LW_PLAIN(x, col, 'u64', 'item:oq') FROM t")
	require.ErrorContains(t, err, "must be a string literal")

	err = expandErr(t, "SELECT LW_PLAIN(x, 'a', 42, 'item:oq') FROM t")
	require.ErrorContains(t, err, "must be a string literal")

	err = expandErr(t, "SELECT LW_PLAIN(x, 'a', 'u64', 'item:oq', 'use:tlp-amber') FROM t")
	require.ErrorContains(t, err, "tagged section")

	err = expandErr(t, "SELECT LW_PLAIN(x, 'a', 'u64') FROM t")
	require.ErrorContains(t, err, "item:")

	err = expandErr(t, "SELECT LW_TV_MEMB(m, 'mysec', 'nope') FROM t")
	require.ErrorContains(t, err, "unknown membership channel")

	err = expandErr(t, "SELECT LW_TV_MEMB(m, 'mysec', 'low-card-ref', 'extra') FROM t")
	require.ErrorContains(t, err, "arity")

	err = expandErr(t, "SELECT LW_PLAIN(x, 'a', 'nosuchtype', 'item:oq') FROM t")
	require.ErrorContains(t, err, "canonical type")
}

// TestExpand_DuplicateMintRejected: two calls in one select minting the same
// physical name — including via spelling folding (myCol ≡ my-col) — are a
// loud error, while UNION members minting the same names are legal (their
// column sets must agree).
func TestExpand_DuplicateMintRejected(t *testing.T) {
	err := expandErr(t, "SELECT LW_PLAIN(a, 'myCol', 'u64', 'item:oq'), LW_PLAIN(b, 'my-col', 'u64', 'item:oq') FROM t")
	require.ErrorContains(t, err, "mint the same physical column name")

	one := "SELECT LW_PLAIN(a, 'x', 'u64', 'item:oq') FROM t"
	out, err := ExpandPass.Run(one + " UNION ALL " + one)
	require.NoError(t, err, "UNION members minting the same name are legal")
	require.Equal(t, 2, strings.Count(out, `AS "oq:x:u64:::0:"`))
}

// TestExpand_SpellingsAndPlacements: call-name matching is case- and
// quoting-insensitive, and the position rule admits every whole-projection-
// item placement — CTE bodies, DISTINCT, LIMIT.
func TestExpand_SpellingsAndPlacements(t *testing.T) {
	for _, in := range []string{
		`SELECT "LW_PLAIN"(x, 'a', 'u64', 'item:oq') FROM t`,
		"SELECT `LW_PLAIN`(x, 'a', 'u64', 'item:oq') FROM t",
		"SELECT Lw_Plain(x, 'a', 'u64', 'item:oq') FROM t",
		"WITH c AS (SELECT LW_PLAIN(x, 'a', 'u64', 'item:oq') FROM t) SELECT * FROM c",
		"SELECT DISTINCT LW_PLAIN(x, 'a', 'u64', 'item:oq') FROM t LIMIT 5",
	} {
		out, err := ExpandPass.Run(in)
		require.NoError(t, err, in)
		require.Contains(t, out, `AS "oq:a:u64:::0:"`, in)
		require.NotContains(t, strings.ToLower(out), "lw_plain", in)
	}
}

// TestExpand_CommentInsideExpression: hidden-channel trivia inside the kept
// expression span survives, and the expanded statement re-parses — a
// trailing line comment must not swallow the emitted alias.
func TestExpand_CommentInsideExpression(t *testing.T) {
	out, err := ExpandPass.Run("SELECT LW_PLAIN(sum(x) -- keep\n + 1, 'a', 'u64', 'item:oq') FROM t")
	require.NoError(t, err)
	require.Contains(t, out, "-- keep")
	_, err = nanopass.Parse(out)
	require.NoError(t, err)

	out, err = ExpandPass.Run("SELECT LW_PLAIN(sum(x) -- tail\n, 'a', 'u64', 'item:oq') FROM t")
	require.NoError(t, err)
	_, err = nanopass.Parse(out)
	require.NoError(t, err, "a trailing line comment must not comment out the alias")
}

// authoringCorpus is the ADR-0181 verification corpus: valid constructor
// calls, nested expressions, several per statement — the shapes the pass
// must expand and stay idempotent on.
func authoringCorpus() []string {
	return []string{
		"SELECT LW_PLAIN(sum(x), 'total-revenue', 'u64', 'item:oq') FROM t",
		"SELECT LW_PLAIN(count(), 'n', 'u64', 'item:oq', 'enc:delta-encoding', 'sem:scale-of-measurement-metric-ratio') FROM t GROUP BY k",
		"SELECT LW_TV(v, 'mysec', 'my-col', 'u64h', 'enc:light-general-compression'), LW_TV_MEMB(m, 'mysec', 'low-card-ref'), LW_TV_SUPPORT(c, 'mysec', 'lrcard') FROM src",
		"SELECT id, LW_TV(if(a > 0, a, b), 'meas', 'reading', 'f64', 'use:tlp-green') FROM src WHERE has(tags, 'x')",
		"SELECT * FROM (SELECT LW_PLAIN(x + 1, 'shifted', 'i64', 'item:oq') FROM t)",
	}
}

// TestExpand_AssertProperties runs the declared-properties harness over the
// shared corpus (the pass must be inert and idempotent on SQL without
// authoring calls) plus the authoring corpus (idempotent after expansion).
func TestExpand_AssertProperties(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	corpus := make([]string, 0, len(entries)+8)
	for _, e := range entries {
		corpus = append(corpus, e.SQL)
	}
	corpus = append(corpus, authoringCorpus()...)
	nanopass.AssertProperties(t, ExpandPass, corpus)
}

// TestExpand_SharedCorpusUntouched pins inertness byte for byte: no entry of
// the shared corpus carries a constructor call, so none may change.
func TestExpand_SharedCorpusUntouched(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	for _, e := range entries {
		out, runErr := ExpandPass.Run(e.SQL)
		require.NoError(t, runErr, e.Name)
		require.Equal(t, e.SQL, out, e.Name)
	}
}

func TestExpand_WithAdoptedSegments(t *testing.T) {
	seg := lwsql.DefaultTableSegments()
	seg.Separator = "_"
	p := ExpandPassWithSegments(seg)
	out, err := p.Run("SELECT LW_TV(v, 'mysec', 'c', 's') FROM t")
	require.NoError(t, err)
	require.Contains(t, out, `v AS "tv_mysec_c_val_s____0__"`)
}

func TestHasAuthoringMarker(t *testing.T) {
	require.True(t, HasAuthoringMarker("select LW_PLAIN(x, 'a', 'u64', 'item:oq')"))
	require.True(t, HasAuthoringMarker("select lw_tv_memb(m, 's', 'low-card-ref')"))
	require.False(t, HasAuthoringMarker("select a, b from t"))
	// Conservative false positive: marker inside a literal costs a parse, nothing else.
	require.True(t, HasAuthoringMarker("select 'lw_plain' from t"))
}
