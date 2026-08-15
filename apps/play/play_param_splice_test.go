package play

// The client-side substitution of SQL-valued placeholders (ADR-0187 (proposed)
// §SD4/§SD6, milestone M2): the per-category splice rules, the error-position
// mapping back onto a field, the directive write-back, and the wire body the
// whole path produces.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

func TestSpliceExprSlots(t *testing.T) {
	cases := []struct {
		name   string
		sql    string
		values map[string]string
		want   string
	}{
		{
			"no values changes nothing",
			"SELECT a FROM t WHERE {cond:Expr}", nil,
			"SELECT a FROM t WHERE {cond:Expr}",
		},
		{
			"an undeclared slot is left for the run gate",
			"SELECT a FROM t WHERE {cond:Expr}", map[string]string{"other": "x"},
			"SELECT a FROM t WHERE {cond:Expr}",
		},
		{
			"an empty value is not a declaration",
			"SELECT a FROM t WHERE {cond:Expr}", map[string]string{"cond": ""},
			"SELECT a FROM t WHERE {cond:Expr}",
		},
		{
			"Expr splices parenthesised",
			"SELECT a FROM t WHERE {cond:Expr}", map[string]string{"cond": "b = 2"},
			"SELECT a FROM t WHERE (b = 2)",
		},
		// The parentheses are the whole point: spliced bare, this reassociates
		// into `x AND b = 2 OR c = 3` — a different query that still parses and
		// still runs, which is the worst kind of wrong.
		{
			"Expr parentheses stop reassociation",
			"SELECT a FROM t WHERE x AND {cond:Expr}", map[string]string{"cond": "b = 2 OR c = 3"},
			"SELECT a FROM t WHERE x AND (b = 2 OR c = 3)",
		},
		// A list cannot be parenthesised without becoming a tuple.
		{
			"ExprList splices bare",
			"SELECT {cols:ExprList} FROM t", map[string]string{"cols": "a AS x, b AS y"},
			"SELECT a AS x, b AS y FROM t",
		},
		{
			"Identifier is not spliced — ClickHouse substitutes it",
			"SELECT a FROM {tbl:Identifier}", map[string]string{"tbl": "system.one"},
			"SELECT a FROM {tbl:Identifier}",
		},
		{
			"a value-typed slot is not spliced",
			"SELECT a FROM t LIMIT {lim:UInt64}", map[string]string{"lim": "10"},
			"SELECT a FROM t LIMIT {lim:UInt64}",
		},
		{
			"two slots in one buffer",
			"SELECT {cols:ExprList} FROM t WHERE {cond:Expr}",
			map[string]string{"cols": "a, b", "cond": "c > 1"},
			"SELECT a, b FROM t WHERE (c > 1)",
		},
		// collectParamSlots dedups by name; the rewriter must not, or a name
		// written twice is substituted once and left dangling once.
		{
			"one name written twice is substituted twice",
			"SELECT a FROM t WHERE {cond:Expr} OR NOT {cond:Expr}",
			map[string]string{"cond": "b = 2"},
			"SELECT a FROM t WHERE (b = 2) OR NOT (b = 2)",
		},
		{
			"a comment declaration survives the splice",
			"-- play: expr cond = b = 2\nSELECT a FROM t WHERE {cond:Expr}",
			map[string]string{"cond": "b = 2"},
			"-- play: expr cond = b = 2\nSELECT a FROM t WHERE (b = 2)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := spliceExprSlots(tc.sql, tc.values)
			require.NoError(t, err)
			require.Equal(t, tc.want, out)
		})
	}
}

// Degrades rather than fails: an unparseable buffer comes back untouched with
// the error, and the caller ships what the user wrote.
func TestSpliceExprSlotsDegradesOnParseFailure(t *testing.T) {
	const broken = "SELECT FROM WHERE {cond:Expr} ((("
	out, spl, err := spliceExprSlots(broken, map[string]string{"cond": "b = 2"})
	require.Error(t, err)
	require.Equal(t, broken, out)
	require.Empty(t, spl)
}

// The recorded output range is the VALUE's own extent, parentheses excluded —
// which is what lets an error offset subtract to a field position.
func TestSpliceRecordsWhereEachValueLanded(t *testing.T) {
	const sql = "SELECT a FROM t WHERE {cond:Expr}"
	out, spl, err := spliceExprSlots(sql, map[string]string{"cond": "b = 2"})
	require.NoError(t, err)
	require.Len(t, spl, 1)
	require.Equal(t, "cond", spl[0].Name)
	require.Equal(t, "b = 2", spl[0].Value)
	require.Equal(t, "b = 2", out[spl[0].Out.Start:spl[0].Out.End],
		"Out addresses the value and not its parentheses")
}

func TestExprMarkFor(t *testing.T) {
	spl := []exprSplice{{
		Name:  "cond",
		Cat:   exprCatExpr,
		Out:   nanopass.SourceRange{Start: 20, End: 30},
		Value: "0123456789",
	}}
	name, mark, ok := exprMarkFor(spl, 24)
	require.True(t, ok)
	require.Equal(t, "cond", name)
	require.Equal(t, nanopass.SourceRange{Start: 4, End: 10}, mark,
		"the mark runs from the error to the end of the value")

	// Outside every spliced value: the fault belongs to the query, and the
	// editor's own underline already has it.
	_, _, ok = exprMarkFor(spl, 5)
	require.False(t, ok)
	_, _, ok = exprMarkFor(spl, 99)
	require.False(t, ok)

	// At the closing edge the value is what failed to finish, so mark all of it.
	_, mark, ok = exprMarkFor(spl, 30)
	require.True(t, ok)
	require.Equal(t, nanopass.SourceRange{Start: 0, End: 10}, mark)
}

// §SD6 end to end: a broken expression underlines in its own field, and a
// buffer whose expressions are fine produces no mark at all.
func TestComputeExprMarks(t *testing.T) {
	const sql = "SELECT a FROM t WHERE {cond:Expr}"

	require.Empty(t, computeExprMarks(sql, map[string]string{"cond": "b = 2"}),
		"a substituted buffer that parses has nothing to mark")

	marks := computeExprMarks(sql, map[string]string{"cond": "b = = 2"})
	require.Len(t, marks, 1)
	mark, has := marks["cond"]
	require.True(t, has, "the fault is inside the value, so it is the field's")
	require.False(t, mark.Empty())
	require.LessOrEqual(t, mark.End, len("b = = 2"), "the mark is in the value's coordinates")

	// A fault outside every spliced value belongs to the query.
	require.Empty(t, computeExprMarks("SELECT a FROM t WHERE {cond:Expr} GROUP", map[string]string{"cond": "b = 2"}))
}

func TestSyncExprDirectives(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		values  map[string]string
		want    string
		changed bool
	}{
		{
			"rewrites in place, keeping position",
			"SET param_x = '1';\n-- play: expr cond = a = 1\nSELECT 1",
			map[string]string{"cond": "b = 2"},
			"SET param_x = '1';\n-- play: expr cond = b = 2\nSELECT 1", true,
		},
		{
			"idempotent",
			"-- play: expr cond = a = 1\nSELECT 1",
			map[string]string{"cond": "a = 1"},
			"-- play: expr cond = a = 1\nSELECT 1", false,
		},
		{
			"an emptied value loses its line",
			"-- play: expr cond = a = 1\nSELECT 1",
			map[string]string{"cond": ""},
			"SELECT 1", true,
		},
		// A new declaration goes UNDER the prelude: above it, it would end the
		// prelude before it starts (§SD3).
		{
			"a new declaration lands below the prelude",
			"SET param_x = '1';\nSELECT 1",
			map[string]string{"cond": "a = 1"},
			"SET param_x = '1';\n-- play: expr cond = a = 1\nSELECT 1", true,
		},
		{
			"a new declaration with no prelude leads the buffer",
			"SELECT 1",
			map[string]string{"cond": "a = 1"},
			"-- play: expr cond = a = 1\nSELECT 1", true,
		},
		{
			"a second declaration joins the first",
			"-- play: expr cond = a = 1\nSELECT 1",
			map[string]string{"cond": "a = 1", "cols": "x, y"},
			"-- play: expr cond = a = 1\n-- play: expr cols = x, y\nSELECT 1", true,
		},
		{
			"a value full of equals round-trips",
			"-- play: expr cond = a = 1\nSELECT 1",
			map[string]string{"cond": "a = 1 AND b = 2"},
			"-- play: expr cond = a = 1 AND b = 2\nSELECT 1", true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, changed := syncExprDirectives(tc.sql, tc.values)
			require.Equal(t, tc.want, out)
			require.Equal(t, tc.changed, changed)
		})
	}
}

// The writer and the reader are one syntax: whatever syncExprDirectives writes,
// scanExprHints must read back unchanged.
func TestExprDirectiveRoundTrip(t *testing.T) {
	values := map[string]string{
		"cond": "status = 'error' AND ts > now() - INTERVAL 1 HOUR",
		"cols": "a AS x, b AS y",
	}
	out, changed := syncExprDirectives("SELECT 1", values)
	require.True(t, changed)
	require.Equal(t, values, scanExprHints(out))
}

// Phase 2→3 through the pane: a field edit rewrites its own declaration, and
// the next parse agrees with it rather than reverting it.
func TestExprDraftDriftWritesTheDirective(t *testing.T) {
	app := paneApp(t, "-- play: expr cond = a = 1\nSELECT x FROM t WHERE {cond:Expr}")
	require.Equal(t, "a = 1", *app.paramDrafts["cond"])

	*app.paramDrafts["cond"] = "b = 2"
	app.syncParamDriftToPrelude()
	require.Contains(t, app.sql, "-- play: expr cond = b = 2")
	require.NotContains(t, app.sql, "SET param_cond", "never the prelude")

	reparse(t, app)
	require.Equal(t, "b = 2", *app.paramDrafts["cond"], "the parse agrees with the edit")
}

// A declared expression is filled: it is substituted before the body reaches
// the wire, so it must not hold the run gate. An undeclared one must.
func TestDeclaredExpressionIsNotUnfilled(t *testing.T) {
	filled := paneApp(t, "-- play: expr cond = a = 1\nSELECT x FROM t WHERE {cond:Expr}")
	require.Empty(t, filled.unfilledInputs())
	filled.caretByte = len(filled.sql)
	runSQL, _, _ := filled.runBuffer()
	_, _, unfilled := filled.resolveRunSignals(runSQL)
	require.Empty(t, unfilled, "the run gate reads the same declaration")

	bare := paneApp(t, "SELECT x FROM t WHERE {cond:Expr}")
	require.Equal(t, []string{"cond"}, bare.unfilledInputs())
	bare.caretByte = len(bare.sql)
	runSQL, _, _ = bare.runBuffer()
	_, _, unfilled = bare.resolveRunSignals(runSQL)
	require.Equal(t, []string{"cond"}, unfilled)
}

// What actually ships. The step runs between the SET-param harvest and the pass
// registry, so the wire body carries the substituted predicate and no trace of
// the placeholder.
func TestWireBodyCarriesTheSubstitutedExpression(t *testing.T) {
	const sql = "SET param_lim = 3;\n" +
		"-- play: expr cond = number % 2 = 0\n" +
		"SELECT number FROM numbers(10) WHERE {cond:Expr} LIMIT {lim:UInt64}"
	cl := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	body, params := cl.BuildStatement(sql)

	require.Contains(t, body, "(number % 2 = 0)", "the predicate is spliced, parenthesised")
	require.NotContains(t, body, "{cond:Expr}", "no placeholder survives to the wire")
	require.Contains(t, body, "{lim:UInt64}", "a value slot still rides the param channel")
	require.Equal(t, "3", params["param_lim"])
	require.NotContains(t, params, "param_cond", "an expression is never a URL parameter")
	require.True(t, strings.HasSuffix(strings.TrimSpace(body), "FORMAT ArrowStream"))
}

// The Preview's "as sent" view is the same code path, so the step reports
// itself there — which is what makes the substitution accountable rather than
// invisible.
func TestSpliceStepIsOnTheRewriteTrace(t *testing.T) {
	cl := NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	obs := cl.RewriteTrace("-- play: expr cond = a = 1\nSELECT x FROM t WHERE {cond:Expr}")
	var found bool
	for _, o := range obs {
		if o.Name == rewriteStepSpliceExpr {
			found = true
			require.True(t, o.Changed, "the step changed the buffer and says so")
		}
	}
	require.True(t, found, "the trace names the splice step")
}
