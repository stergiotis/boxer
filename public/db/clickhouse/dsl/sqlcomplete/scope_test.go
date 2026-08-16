package sqlcomplete_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func siteAndCaret(t *testing.T, buf string) (sql string, site highlight.CaretSite, caret int) {
	t.Helper()
	i := strings.Index(buf, caret1)
	require.GreaterOrEqualf(t, i, 0, "case %q has no caret marker", buf)
	sql = buf[:i] + buf[i+len(caret1):]
	return sql, highlight.SiteAtIn(sql, i), i
}

const caret1 = "|"

func TestRepairAttempts(t *testing.T) {
	cases := []struct {
		buf   string
		first string
	}{
		{
			buf:   `SELECT LW_COMPONENT('SysMem') AS m, tupleElement(m, '|`,
			first: `SELECT LW_COMPONENT('SysMem') AS m, tupleElement(m, '__caret__')`,
		},
		{
			buf:   `SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot|`,
			first: `SELECT tupleElement(LW_COMPONENT('SysMem'), '__caret__')`,
		},
		{
			buf:   `SELECT LW_COMPONENT('|') FROM boxer.facts`,
			first: `SELECT LW_COMPONENT('__caret__') FROM boxer.facts`,
		},
		{
			buf:   `SELECT toHour(ts, |`,
			first: `SELECT toHour(ts, __caret__)`,
		},
		{
			buf:   `SELECT m.| FROM boxer.facts`,
			first: `SELECT m.__caret__ FROM boxer.facts`,
		},
		{
			// A terminated literal the caret moved back into: the whole
			// literal is replaced, and the tail survives.
			buf:   `SELECT LW_COMPONENT('Sys|Mem') FROM t`,
			first: `SELECT LW_COMPONENT('__caret__') FROM t`,
		},
		{
			buf:   `WITH c AS (SELECT 1 AS q) SELECT tupleElement(m, '|`,
			first: `WITH c AS (SELECT 1 AS q) SELECT tupleElement(m, '__caret__')`,
		},
	}
	for _, c := range cases {
		t.Run(c.buf, func(t *testing.T) {
			sql, site, caret := siteAndCaret(t, c.buf)
			attempts, at := sqlcomplete.Repair(sql, site, caret)
			require.NotEmpty(t, attempts)
			assert.Equal(t, c.first, attempts[0])
			require.Len(t, at, len(attempts))
			for i := range attempts {
				assert.Truef(t, strings.Contains(attempts[i][at[i]:], sqlcomplete.Sentinel),
					"attempt %d does not carry the sentinel where it says", i)
			}
		})
	}
}

// The two attempts differ exactly when there is a tail to cut.
func TestRepairSecondAttemptCutsTheTail(t *testing.T) {
	sql, site, caret := siteAndCaret(t, `SELECT LW_COMPONENT('|') , nonsense FROM`)
	attempts, _ := sqlcomplete.Repair(sql, site, caret)
	require.Len(t, attempts, 2)
	assert.Contains(t, attempts[0], "nonsense")
	assert.NotContains(t, attempts[1], "nonsense")
	assert.Equal(t, `SELECT LW_COMPONENT('__caret__')`, attempts[1])
}

func TestParseScopeAnswers(t *testing.T) {
	t.Run("the caret's call frame", func(t *testing.T) {
		sql, site, caret := siteAndCaret(t, `SELECT tupleElement(LW_COMPONENT('SysMem'), '|`)
		sc, err := sqlcomplete.ParseScope(sql, site, caret)
		require.NoError(t, err)
		require.NotNil(t, sc.Frame)
		assert.Equal(t, "tupleElement", sc.Frame.Callee)
		assert.Equal(t, 1, sc.Frame.Ordinal, "the tree agrees with the site about the ordinal")
		assert.Equal(t, "SELECT", sc.Clause)
	})

	t.Run("aliases with their defining expressions", func(t *testing.T) {
		sql, site, caret := siteAndCaret(t,
			`SELECT LW_COMPONENT('SysMem') AS m, CAST(x AS Nullable(Float32)) AS y, tupleElement(m, '|`)
		sc, err := sqlcomplete.ParseScope(sql, site, caret)
		require.NoError(t, err)
		assert.Equal(t, "LW_COMPONENT('SysMem')", sc.Aliases["m"])
		// The whitespace survives, which the keyword cast spelling needs.
		assert.Equal(t, "CAST(x AS Nullable(Float32))", sc.Aliases["y"])
	})

	t.Run("tables and CTEs", func(t *testing.T) {
		sql, site, caret := siteAndCaret(t,
			`WITH c AS (SELECT 1 AS q) SELECT tupleElement(m, '|') FROM boxer.facts AS f`)
		sc, err := sqlcomplete.ParseScope(sql, site, caret)
		require.NoError(t, err)
		require.Len(t, sc.Tables, 1)
		assert.Equal(t, "boxer", sc.Tables[0].Database)
		assert.Equal(t, "facts", sc.Tables[0].Name)
		assert.Equal(t, "f", sc.Tables[0].Alias)
		assert.Contains(t, sc.CTEs, "c")

		ref, ok := sc.LookupTable("f")
		require.True(t, ok)
		assert.Equal(t, "facts", ref.Name)
	})

	t.Run("the clause a caret landed in", func(t *testing.T) {
		for _, c := range []struct{ buf, clause string }{
			{`SELECT * FROM boxer.facts WHERE tupleElement(LW_COMPONENT('SysMem'), '|') > 1`, "WHERE"},
			{`SELECT tupleElement(m, '|') FROM t`, "SELECT"},
		} {
			sql, site, caret := siteAndCaret(t, c.buf)
			sc, err := sqlcomplete.ParseScope(sql, site, caret)
			require.NoErrorf(t, err, "%s", c.buf)
			assert.Equalf(t, c.clause, sc.Clause, "%s", c.buf)
		}
	})
}

// The positions the design already knows the repair cannot save: a JOIN wants
// ON or USING, and grammar1 has no dot form on a call result until §SD11.
// Both are silences the site alone still covers.
func TestParseScopeDeclinesWhereTheGrammarCannotHelp(t *testing.T) {
	for _, buf := range []string{
		`SELECT a FROM t JOIN |`,
	} {
		t.Run(buf, func(t *testing.T) {
			sql, site, caret := siteAndCaret(t, buf)
			_, err := sqlcomplete.ParseScope(sql, site, caret)
			assert.Error(t, err)
		})
	}
}

// The typer follows the scope's alias map, which is the whole point of having
// one: `AS m` then `tupleElement(m, …)` is the driving case §SD5 named.
func TestScopeFeedsTheTyper(t *testing.T) {
	e := testEngine(t)
	sql, site, caret := siteAndCaret(t, `SELECT LW_COMPONENT('SysMem') AS m, tupleElement(m, 'Tot|`)
	sc, err := sqlcomplete.ParseScope(sql, site, caret)
	require.NoError(t, err)

	res := e.Complete(sqlcomplete.Request{Site: site, Scope: sc, Statement: sql, Caret: caret})
	require.Empty(t, res.Silent)
	assert.Equal(t, []string{"Id", "Ts", "TotalBytes", "TotalPercent", "FreeBytes"}, texts(res))
	assert.Equal(t, sqlcomplete.MatchPrefix, res.Match)
}

// `m.` on a tuple-typed alias, and `t.` on a table alias — SD7's two identifier
// receivers.
func TestScopeAnswersMemberAccess(t *testing.T) {
	e := testEngine(t)

	sql, site, caret := siteAndCaret(t, `SELECT LW_COMPONENT('SysMem') AS m, m.Tot| FROM t`)
	sc, err := sqlcomplete.ParseScope(sql, site, caret)
	require.NoError(t, err)
	res := e.Complete(sqlcomplete.Request{Site: site, Scope: sc, Statement: sql, Caret: caret})
	require.Empty(t, res.Silent)
	assert.Contains(t, texts(res), "TotalBytes")

	// A table alias resolves to the column domain, which this fixture's
	// catalog cannot answer — so it is silent, with the catalog's reason.
	sql, site, caret = siteAndCaret(t, `SELECT t.| FROM boxer.facts AS t`)
	sc, err = sqlcomplete.ParseScope(sql, site, caret)
	require.NoError(t, err)
	res = e.Complete(sqlcomplete.Request{Site: site, Scope: sc, Statement: sql, Caret: caret})
	assert.Empty(t, res.Items)
	assert.Contains(t, res.Silent, "column")
}

// A keyword-syntax call has no comma to count, so the tree's ordinal is the
// only one — and it arrives with the scope (§SD3).
func TestKeywordSyntaxOrdinalComesFromTheTree(t *testing.T) {
	e := testEngine(t)
	e.Providers.Catalog.TypeNames = func() ([]sqlcomplete.Item, bool) {
		return items("UInt8", "String"), true
	}

	sql, site, caret := siteAndCaret(t, `SELECT CAST(x AS |`)
	bare := e.Complete(sqlcomplete.Request{Site: site, Statement: sql, Caret: caret})
	assert.Contains(t, bare.Silent, "keyword-syntax call")

	sc, err := sqlcomplete.ParseScope(sql, site, caret)
	require.NoError(t, err)
	require.NotNil(t, sc.Frame)
	assert.Equal(t, "CAST", sc.Frame.Callee)

	withScope := e.Complete(sqlcomplete.Request{Site: site, Scope: sc, Statement: sql, Caret: caret})
	assert.Equal(t, 1, withScope.Ordinal, "the tree says this is the type argument")
}

// A scope that landed on another call does not answer for this one: the caret
// can move between the launch and the drain.
func TestScopeFrameMustNameTheSameCall(t *testing.T) {
	e := testEngine(t)
	sql, site, caret := siteAndCaret(t, `SELECT CAST(x AS |`)
	stale := &sqlcomplete.Scope{Frame: &highlight.CallFrame{Callee: "toDateTime", Ordinal: 1}}
	res := e.Complete(sqlcomplete.Request{Site: site, Scope: stale, Statement: sql, Caret: caret})
	assert.Contains(t, res.Silent, "keyword-syntax call")
}
