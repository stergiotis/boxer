package play

// The SQL-expression knob (ADR-0187 (proposed) §SD1/§SD2/§SD3, milestone M1):
// the category classifier, the `-- play: expr` scanner, the advisory lines, and
// the value-path gates that keep a spliced draft out of the prelude and out of
// the signal store.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
	"github.com/stretchr/testify/require"
)

func TestExprCategoryFor(t *testing.T) {
	cases := []struct {
		typeExpr string
		want     paramExprCategoryE
	}{
		{"Expr", exprCatExpr},
		{"ExprList", exprCatList},
		{"Identifier", exprCatIdentifier},
		// Case-insensitive: a reader who wrote the category in the wrong case
		// meant the category, and a silent text field is the worst answer.
		{"expr", exprCatExpr},
		{"EXPRLIST", exprCatList},
		{"identifier", exprCatIdentifier},
		{"  Expr  ", exprCatExpr},
		// Exact word only. A parameterised type is not one of these, and
		// guessing would invent a category with no substitution rule.
		{"Nullable(Expr)", exprCatNone},
		{"Expression", exprCatNone},
		{"Exp", exprCatNone},
		{"UInt64", exprCatNone},
		{"String", exprCatNone},
		{"", exprCatNone},
	}
	for _, tc := range cases {
		t.Run(tc.typeExpr, func(t *testing.T) {
			require.Equal(t, tc.want, exprCategoryFor(tc.typeExpr))
		})
	}
}

// Only Expr and ExprList are substituted client-side; Identifier is a real
// ClickHouse parameter and rides the untouched value path (§SD2). Every
// value-path fork keys on this, so it is pinned on its own.
func TestExprCategorySplicedSplitsTheTwoMechanisms(t *testing.T) {
	require.True(t, exprCatExpr.spliced())
	require.True(t, exprCatList.spliced())
	require.False(t, exprCatIdentifier.spliced(), "Identifier is a ClickHouse param, not a splice")
	require.False(t, exprCatNone.spliced())
}

func TestScanExprHints(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want map[string]string
	}{
		{"none", "SELECT 1", nil},
		{
			"plain",
			"-- play: expr cond = status = 'error'\nSELECT a FROM t WHERE {cond:Expr}",
			map[string]string{"cond": "status = 'error'"},
		},
		// The first `=` is the separator and the rest is the value. An
		// expression full of `=` is the ordinary case, not the edge case.
		{
			"value keeps every later equals",
			"-- play: expr cond = a = 1 AND b = 2",
			map[string]string{"cond": "a = 1 AND b = 2"},
		},
		{"no spaces around the separator", "-- play: expr cond=a=1", map[string]string{"cond": "a=1"}},
		{"value trimmed at both ends", "-- play: expr cond =   a = 1   ", map[string]string{"cond": "a = 1"}},
		{"marker case-insensitive", "-- PLAY: EXPR cond = a = 1", map[string]string{"cond": "a = 1"}},
		{"leading whitespace tolerated", "   \t-- play: expr cond = a = 1", map[string]string{"cond": "a = 1"}},
		// An empty value is not a declaration: a slot's position is mandatory
		// and `WHERE ()` is not a query, so it stays unfilled.
		{"empty value is not a declaration", "-- play: expr cond =", nil},
		{"empty value with spaces", "-- play: expr cond =    ", nil},
		{"no separator", "-- play: expr cond", nil},
		{"no name", "-- play: expr  = a = 1", nil},
		// A half-typed marker must not make the pane shout.
		{"not a marker", "-- play: expression cond = a = 1", nil},
		{"not a comment", "play: expr cond = a = 1", nil},
		{
			"first hint for a name wins",
			"-- play: expr cond = a = 1\n-- play: expr cond = b = 2",
			map[string]string{"cond": "a = 1"},
		},
		{
			"two slots",
			"-- play: expr cond = a = 1\n-- play: expr cols = x AS p, y AS q",
			map[string]string{"cond": "a = 1", "cols": "x AS p, y AS q"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, scanExprHints(tc.sql))
		})
	}
}

func TestOrphanExprHints(t *testing.T) {
	slots := []paramSlot{
		{Name: "cond", Type: "Expr"},
		{Name: "cols", Type: "ExprList"},
		{Name: "tbl", Type: "Identifier"},
		{Name: "lim", Type: "UInt64"},
	}
	cases := []struct {
		name  string
		hints map[string]string
		want  []string
	}{
		{"no hints", nil, nil},
		{"all claimed", map[string]string{"cond": "a = 1", "cols": "x AS p"}, nil},
		{"name the buffer does not carry", map[string]string{"nosuch": "a = 1"}, []string{"nosuch"}},
		// Same symptom, different mistake: the slot exists but is not a
		// spliced category, so the declaration is ignored just as silently.
		{"name of a value-typed slot", map[string]string{"lim": "5"}, []string{"lim"}},
		{"name of an Identifier slot", map[string]string{"tbl": "system.numbers"}, []string{"tbl"}},
		{
			"stable order",
			map[string]string{"zeta": "1", "alpha": "2"},
			[]string{"alpha", "zeta"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, orphanExprHints(tc.hints, slots))
		})
	}
}

func TestOrphanExprNote(t *testing.T) {
	require.Equal(t, "", orphanExprNote(nil))
	require.Contains(t, orphanExprNote([]string{"cond"}), "cond")
	require.Contains(t, orphanExprNote([]string{"cond"}), "the declaration is ignored")
}

// The gate that matters most: a spliced slot must never reach the `param_*`
// prelude, because ExtractParams harvests every one of those onto the URL and
// the server would receive an expression as a string.
func TestSplicedSlotStaysOutOfThePrelude(t *testing.T) {
	sql := "-- play: expr cond = status = 'error'\nSELECT a FROM t WHERE {cond:Expr}"
	app := paneApp(t, sql)

	// No PRELUDE tier — the mirror this test is about. It is nonetheless
	// pinned, by its own declaration: M3 gave paramPinned a second source, so
	// the bit no longer means "has a SET" (see TestExprTierBitReadsTheDeclaration).
	_, synced := app.paramSyncedValues["cond"]
	require.False(t, synced, "a spliced slot never enters the prelude mirror")
	require.True(t, app.paramPinned("cond"), "but a declaration pins it")

	// Phase 2: the widget mutates its draft. Phase 3 sends that to the
	// directive (TestExprDraftDriftWritesTheDirective), and to neither of the
	// two places it must never reach.
	draft, has := app.paramDrafts["cond"]
	require.True(t, has)
	*draft = "status = 'warn'"
	app.syncParamDriftToPrelude()

	require.NotContains(t, app.sql, "SET param_cond", "never the prelude")
	_, published := app.graph.signals().Get("cond")
	require.False(t, published, "and never the signal store — that is M3's live tier")
}

// The declaration wins over the draft on every parse, exactly as a prelude
// value does at the pinned tier.
//
// M1 asserted the opposite — seed once, never re-seed — because it had no
// write-back and re-seeding would have reverted each keystroke. M2 gave the
// directive its write-back, so drift rewrites the declaration BEFORE this parse
// reads it and the overwrite is a no-op. Inverted rather than deleted, so the
// record shows the M1 rule was retired deliberately.
func TestSplicedDraftFollowsTheDeclaration(t *testing.T) {
	sql := "-- play: expr cond = a = 1\nSELECT x FROM t WHERE {cond:Expr}"
	app := paneApp(t, sql)

	draft, has := app.paramDrafts["cond"]
	require.True(t, has)
	require.Equal(t, "a = 1", *draft, "the declaration seeds the draft")

	// A draft moved WITHOUT its drift being synced is the transient mid-frame
	// state; the next parse restores what the buffer says, because the buffer
	// is the record.
	*draft = "b = 2"
	reparse(t, app)
	require.Equal(t, "a = 1", *app.paramDrafts["cond"], "the parser wins, as it does for a prelude value")
}

// Identifier is the half that already works: a real ClickHouse parameter, so it
// keeps ADR-0124 §SD4's prelude path untouched and only gains a better editor.
func TestIdentifierSlotKeepsThePreludePath(t *testing.T) {
	sql := "SET param_tbl = 'system.numbers';\nSELECT a FROM {tbl:Identifier}"
	app := paneApp(t, sql)

	require.True(t, app.paramPinned("tbl"), "an Identifier binds through the prelude like any value")
	require.Equal(t, "system.numbers", *app.paramDrafts["tbl"])

	*app.paramDrafts["tbl"] = "system.one"
	app.syncParamDriftToPrelude()
	require.Contains(t, app.sql, "SET param_tbl = 'system.one'")
}

func TestExprWidgetMatchesEveryCategoryAndPassesOnValues(t *testing.T) {
	w := newExprWidget()

	_, ok := w.Matches([]paramSlot{{Name: "lim", Type: "UInt64"}})
	require.False(t, ok, "a value-typed slot falls through to the tail")

	// One at a time, so the dispatch loop can hand it several in a row.
	slots := []paramSlot{
		{Name: "lim", Type: "UInt64"},
		{Name: "cond", Type: "Expr"},
		{Name: "cols", Type: "ExprList"},
	}
	idx, ok := w.Matches(slots)
	require.True(t, ok)
	require.Equal(t, []int{1}, idx)

	// Matched on the type alone — an undeclared slot is still a SQL knob, and
	// offering a text field would be the wrong editor exactly when the author
	// is about to write the expression.
	require.Empty(t, w.hints)

	idx, ok = w.Matches([]paramSlot{{Name: "tbl", Type: "Identifier"}})
	require.True(t, ok)
	require.Equal(t, []int{0}, idx)
}

// A renamed placeholder must not keep its Field alive: the field memoises a
// retained lex job against text nobody can see any more.
func TestExprWidgetPrunesFieldsForAbsentSlots(t *testing.T) {
	w := newExprWidget()
	w.fields = map[string]*sqleditor.Field{
		"cond": sqleditor.NewField(),
		"cols": sqleditor.NewField(),
	}
	w.ClearStateForAbsent(map[string]struct{}{"cond": {}})
	require.Len(t, w.fields, 1)
	require.Contains(t, w.fields, "cond")

	// Tolerates having drawn nothing yet.
	require.NotPanics(t, func() { newExprWidget().ClearStateForAbsent(nil) })
}

// §SD3 puts directive lines in the residual — BELOW the `SET` prelude. That is
// the canonical placement, and `SyncParamPrelude` normalises to it by rebuilding
// the buffer as prelude-then-residual; it is no longer load-bearing.
//
// It used to be. `env.harvestSetPrelude` took only a LEADING run of `SET` lines,
// so a comment above them ended the prelude before it started: `BodyOffset`
// collapsed to 0, the buffer read as two statements, and a run under the cursor
// shipped the body WITHOUT its `SET param_*` lines — every parameter then
// reading as unfilled, including ones the buffer plainly binds. ADR-0006's
// 2026-08-15 Update fixed that in the shared prelude definition. Both orders are
// asserted here so the placement cannot quietly become load-bearing again.
func TestExprDirectivesWorkEitherSideOfThePrelude(t *testing.T) {
	const body = "SELECT {cols:ExprList}, {tbl:Identifier}\nFROM numbers(12)\nWHERE {cond:Expr}"
	const directives = "-- play: expr cond = number % 3 = 0\n-- play: expr cols = number AS n\n"
	const prelude = "SET param_tbl = 'number';\n"

	// The declarations are found wherever they sit — the scanner is per line.
	require.Len(t, scanExprHints(prelude+directives+body), 2)
	require.Len(t, scanExprHints(directives+prelude+body), 2)

	for name, sql := range map[string]string{
		"directives below the prelude": prelude + directives + body,
		"directives above the prelude": directives + prelude + body,
	} {
		t.Run(name, func(t *testing.T) {
			app := paneApp(t, sql)
			app.caretByte = len(app.sql)
			runSQL, _, _ := app.runBuffer()
			_, bound, unfilled := app.resolveRunSignals(runSQL)
			require.True(t, bound["tbl"], "the prelude binds on either side of the directives")
			require.Empty(t, unfilled, "and the declared expressions do not hold the gate")
		})
	}
}

// The hint text is the only place a reader is told what shape the knob wants,
// so each category says something different.
func TestExprHintTextDistinguishesTheCategories(t *testing.T) {
	one := exprHintTextFor(exprCatExpr, "cond")
	list := exprHintTextFor(exprCatList, "cols")
	ident := exprHintTextFor(exprCatIdentifier, "tbl")
	require.NotEqual(t, one, list)
	require.NotEqual(t, one, ident)
	require.True(t, strings.Contains(one, "{cond}"))
	require.True(t, strings.Contains(list, "{cols}"))
	require.True(t, strings.Contains(ident, "{tbl}"))
}
