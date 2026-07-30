package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// caretAt turns a marked-up statement into (text, caret): the first `|` is the
// caret and is removed. Keeps the cases readable — every one of them is really
// a claim about where the caret is.
func caretAt(t *testing.T, marked string) (text string, caret int) {
	t.Helper()
	caret = strings.Index(marked, "|")
	if caret < 0 {
		t.Fatalf("case has no caret marker: %q", marked)
	}
	return marked[:caret] + marked[caret+1:], caret
}

// runAtCaret is the whole path under test: split the statement, pick the unit
// the caret is in, compose it.
func runAtCaret(t *testing.T, marked string) (run string, narrowed bool) {
	t.Helper()
	text, caret := caretAt(t, marked)
	unit, ok := pickSubquery(parseSubqueryUnits(text), caret)
	if !ok {
		return "", false
	}
	return unit.compose(text), !unit.Root
}

func TestSubqueryPickInnermost(t *testing.T) {
	cases := []struct {
		name     string
		marked   string
		want     string
		narrowed bool
	}{{
		name:     "caret in a FROM subquery",
		marked:   "SELECT x FROM (SELECT |number AS x FROM numbers(10))",
		want:     "SELECT number AS x FROM numbers(10)",
		narrowed: true,
	}, {
		name:     "caret in the outer select of the same statement",
		marked:   "SELECT |x FROM (SELECT number AS x FROM numbers(10))",
		want:     "SELECT x FROM (SELECT number AS x FROM numbers(10))",
		narrowed: false,
	}, {
		name:     "caret in an expression subquery",
		marked:   "SELECT 1 WHERE 1 IN (SELECT number FROM |numbers(3))",
		want:     "SELECT number FROM numbers(3)",
		narrowed: true,
	}, {
		name:     "innermost of three levels wins",
		marked:   "SELECT * FROM (SELECT * FROM (SELECT |1 AS a))",
		want:     "SELECT 1 AS a",
		narrowed: true,
	}, {
		name:     "the middle level, when the caret is in it",
		marked:   "SELECT * FROM (SELECT * |FROM (SELECT 1 AS a))",
		want:     "SELECT * FROM (SELECT 1 AS a)",
		narrowed: true,
	}, {
		name:     "a caret just past the subquery's last token still belongs to it",
		marked:   "SELECT x FROM (SELECT 1 AS x|)",
		want:     "SELECT 1 AS x",
		narrowed: true,
	}, {
		name:     "a union subquery ships the whole chain",
		marked:   "SELECT * FROM (SELECT 1 AS a UNION ALL SELECT |2)",
		want:     "SELECT 1 AS a UNION ALL SELECT 2",
		narrowed: true,
	}, {
		name:     "a plain statement has only its own query",
		marked:   "SELECT |1",
		want:     "SELECT 1",
		narrowed: false,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run, narrowed := runAtCaret(t, tc.marked)
			if run != tc.want {
				t.Errorf("run:\n got %q\nwant %q", run, tc.want)
			}
			if narrowed != tc.narrowed {
				t.Errorf("narrowed = %v, want %v", narrowed, tc.narrowed)
			}
		})
	}
}

// Exactly one unit is the statement's own query, and it is the one hanging off
// the outermost `query` — NOT the first the walk reaches, nor everything at
// nesting depth 1. `query: setStmt* ctes? selectUnionStmt` puts the CTE clause
// first, so a top-level CTE body is visited earlier and sits at the same depth;
// classifying by either made a caret in a CTE body report "nothing to narrow
// to", which is the case run-subquery exists for.
func TestSubqueryRootIsTheStatementsOwnQuery(t *testing.T) {
	const s = "WITH recent AS (SELECT number AS n FROM numbers(50)) SELECT r.n FROM recent r"
	units := parseSubqueryUnits(s)
	var roots []string
	for _, u := range units {
		if u.Root {
			roots = append(roots, s[u.Src.Start:u.Src.End])
		}
	}
	if len(roots) != 1 {
		t.Fatalf("got %d root units %q, want exactly 1", len(roots), roots)
	}
	if roots[0] != "SELECT r.n FROM recent r" {
		t.Errorf("root = %q, want the statement's own query", roots[0])
	}
}

// …and the caret-level consequence: a top-level CTE body narrows.
func TestSubqueryCaretInTopLevelCteBodyNarrows(t *testing.T) {
	run, narrowed := runAtCaret(t,
		"WITH recent AS (SELECT number AS |n FROM numbers(50)) SELECT r.n FROM recent r")
	if !narrowed {
		t.Error("a caret in a top-level CTE body must narrow to that body")
	}
	if want := "SELECT number AS n FROM numbers(50)"; run != want {
		t.Errorf("run:\n got %q\nwant %q", run, want)
	}
}

func TestSubqueryHoistsEnclosingWith(t *testing.T) {
	cases := []struct {
		name   string
		marked string
		want   string
	}{{
		name:   "a top-level CTE reaches a nested subquery",
		marked: "WITH t AS (SELECT 1 AS a) SELECT * FROM (SELECT a FROM |t)",
		want:   "WITH t AS (SELECT 1 AS a) SELECT a FROM t",
	}, {
		name:   "a scalar WITH item is hoisted too",
		marked: "WITH 7 AS k SELECT * FROM (SELECT |k)",
		want:   "WITH 7 AS k SELECT k",
	}, {
		name:   "two levels of WITH flatten into one list, outermost first",
		marked: "WITH a AS (SELECT 1) SELECT * FROM (WITH b AS (SELECT 2) SELECT * FROM (SELECT * FROM |a, b))",
		want:   "WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a, b",
	}, {
		name:   "the unit's own WITH continues the hoisted list",
		marked: "WITH a AS (SELECT 1) SELECT * FROM (WITH b AS (SELECT 2) SELECT |* FROM b)",
		want:   "WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM b",
	}, {
		name:   "RECURSIVE anywhere in the chain survives the flattening",
		marked: "WITH RECURSIVE a AS (SELECT 1) SELECT * FROM (SELECT * FROM |a)",
		want:   "WITH RECURSIVE a AS (SELECT 1) SELECT * FROM a",
	}, {
		name:   "a CTE body sees its siblings, not itself",
		marked: "WITH a AS (SELECT 1 AS x), b AS (SELECT |x FROM a) SELECT * FROM b",
		want:   "WITH a AS (SELECT 1 AS x) SELECT x FROM a",
	}, {
		// ClickHouse binds the names of one WITH level regardless of order, so
		// a body referencing a sibling defined after it is a running statement
		// — the later sibling must travel or the narrowed run invents an
		// unknown-table error (found live; the differential test pins it).
		name:   "a CTE body sees a sibling defined after it",
		marked: "WITH a AS (SELECT * FROM |b), b AS (SELECT 1 AS one) SELECT * FROM a",
		want:   "WITH b AS (SELECT 1 AS one) SELECT * FROM b",
	}, {
		name:   "an inner rebinding wins, at the outer position",
		marked: "WITH t AS (SELECT 1) SELECT * FROM (WITH t AS (SELECT 2) SELECT |* FROM t)",
		want:   "WITH t AS (SELECT 2) SELECT * FROM t",
	}, {
		// Scalar aliases collide on the server exactly like CTE names
		// (MULTIPLE_EXPRESSIONS_FOR_ALIAS), so they take the same inner-wins
		// deduplication — here via the unit's own clause…
		name:   "a scalar alias the unit rebinds itself is not hoisted",
		marked: "WITH 7 AS k SELECT * FROM (WITH 8 AS k SELECT |k)",
		want:   "WITH 8 AS k SELECT k",
	}, {
		// …and here via an enclosing scope's rebinding.
		name:   "an inner scalar rebinding wins, at the outer position",
		marked: "WITH 7 AS k SELECT * FROM (WITH 8 AS k SELECT * FROM (SELECT |k))",
		want:   "WITH 8 AS k SELECT k",
	}, {
		// The two kinds are separate namespaces: the server lets a named query
		// and a scalar alias share one name, each answering in its own
		// positions, so neither may deduplicate the other away.
		name:   "a named query and a scalar alias sharing a name both travel",
		marked: "WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH 7 AS t SELECT |t FROM t)",
		want:   "WITH t AS (SELECT 1 AS v), 7 AS t SELECT t FROM t",
	}, {
		// A non-recursive rebinding is not visible in its own body — the
		// reference resolves to the OUTER definition, which travels.
		name:   "a non-recursive rebinding's body reaches the outer name",
		marked: "WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH t AS (SELECT v+1 AS v FROM |t) SELECT * FROM t)",
		want:   "WITH t AS (SELECT 1 AS v) SELECT v+1 AS v FROM t",
	}, {
		name:   "the statement's own query is never rewritten",
		marked: "WITH t AS (SELECT 1) SELECT |* FROM t",
		want:   "WITH t AS (SELECT 1) SELECT * FROM t",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run, _ := runAtCaret(t, tc.marked)
			if run != tc.want {
				t.Errorf("run:\n got %q\nwant %q", run, tc.want)
			}
		})
	}
}

// The hoisted output has to be SQL the server will accept — a flattening that
// produced a duplicate CTE name or a stray second WITH would only fail at the
// endpoint, which is exactly where this feature cannot afford to fail.
func TestSubqueryCompositionReparses(t *testing.T) {
	markedCases := []string{
		"WITH t AS (SELECT 1 AS a) SELECT * FROM (SELECT a FROM |t)",
		"WITH a AS (SELECT 1) SELECT * FROM (WITH b AS (SELECT 2) SELECT |* FROM b)",
		"WITH t AS (SELECT 1) SELECT * FROM (WITH t AS (SELECT 2) SELECT |* FROM t)",
		"WITH RECURSIVE a AS (SELECT 1) SELECT * FROM (SELECT * FROM |a)",
		"WITH 7 AS k SELECT * FROM (SELECT |k)",
		"SELECT * FROM (SELECT 1 AS a UNION ALL SELECT |2)",
		"WITH a AS (SELECT * FROM |b), b AS (SELECT 1 AS one) SELECT * FROM a",
		"WITH 7 AS k SELECT * FROM (WITH 8 AS k SELECT |k)",
		"WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH 7 AS t SELECT |t FROM t)",
		"WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH t AS (SELECT v+1 AS v FROM |t) SELECT * FROM t)",
	}
	for _, marked := range markedCases {
		run, _ := runAtCaret(t, marked)
		if pos := firstSyntaxError(run); pos.Ok {
			t.Errorf("composed run does not parse: %v\n  from %q\n  got  %q", pos, marked, run)
		}
	}
}

// Correlated references are the failure hoisting cannot repair, so the model
// has to find them exactly — a missed one is a server error the editor
// promised would not happen, a spurious one warns off a query that runs fine.
func TestSubqueryUnresolvedRefs(t *testing.T) {
	cases := []struct {
		name   string
		marked string
		want   []string
	}{{
		name:   "a correlated reference to an outer alias",
		marked: "SELECT a.x FROM t a WHERE a.y IN (SELECT z FROM u WHERE u.w = |a.x)",
		want:   []string{"a"},
	}, {
		name:   "a reference the unit binds itself is fine",
		marked: "SELECT a.x FROM t a WHERE a.y IN (SELECT u.z FROM u WHERE |u.w = 1)",
		want:   nil,
	}, {
		name:   "a reference to a CTE that travels with the unit is fine",
		marked: "WITH c AS (SELECT 1 AS x) SELECT * FROM (SELECT |c.x FROM c)",
		want:   nil,
	}, {
		name:   "an unbound qualifier is left alone — it may be a tuple field",
		marked: "SELECT a.x FROM t a WHERE a.y IN (SELECT |tup.field FROM u)",
		want:   nil,
	}, {
		name:   "the statement's own query correlates with nothing",
		marked: "SELECT a.x FROM t a WHERE |a.y > 1",
		want:   nil,
	}, {
		name:   "two distinct outer qualifiers are both reported",
		marked: "SELECT 1 FROM t a, s b WHERE a.k IN (SELECT z FROM u WHERE u.p = a.q AND u.r = |b.q)",
		want:   []string{"a", "b"},
	}, {
		// A recursive body naming itself is the one WITH item that can never
		// travel — the definition would be in terms of the text being shipped
		// — so the reference is marked like a correlated qualifier.
		name:   "a recursive body's reference to itself cannot travel",
		marked: "WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM |r WHERE n < 5) SELECT * FROM r",
		want:   []string{"r"},
	}, {
		// Without RECURSIVE the definition is not visible in its own body:
		// the reference binds the OUTER t, which travels, and the mark would
		// warn off a narrowing that runs.
		name:   "a non-recursive rebinding is not a self-reference",
		marked: "WITH t AS (SELECT 1 AS v) SELECT * FROM (WITH t AS (SELECT v+1 AS v FROM |t) SELECT * FROM t)",
		want:   nil,
	}, {
		// Referencing a recursive definition from OUTSIDE its body is the
		// ordinary hoist case — the definition travels whole.
		name:   "a recursive CTE referenced from outside its body is fine",
		marked: "WITH RECURSIVE r AS (SELECT 1 AS n) SELECT * FROM (SELECT * FROM |r)",
		want:   nil,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, caret := caretAt(t, tc.marked)
			unit, ok := pickSubquery(parseSubqueryUnits(text), caret)
			if !ok {
				t.Fatal("no unit resolved")
			}
			var got []string
			for _, r := range unit.Unresolved {
				got = append(got, text[r.Start:r.End])
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("unresolved = %v, want %v", got, tc.want)
			}
		})
	}
}

// The hoisted WITH items are what the editor tints as the carried environment,
// so their spans have to name the items themselves — not the clause, and not
// the whole statement.
func TestSubqueryWithItemSpans(t *testing.T) {
	text, caret := caretAt(t,
		"WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM (SELECT |* FROM a, b)")
	unit, ok := pickSubquery(parseSubqueryUnits(text), caret)
	if !ok {
		t.Fatal("no unit resolved")
	}
	var got []string
	for _, r := range unit.WithItems {
		got = append(got, text[r.Start:r.End])
	}
	want := []string{"a AS (SELECT 1)", "b AS (SELECT 2)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("with items = %v, want %v", got, want)
	}
}

// A buffer mid-edit has no CST, and the caret has nothing to narrow within.
func TestSubqueryUnparseableStatement(t *testing.T) {
	if units := parseSubqueryUnits("SELECT * FROM ("); units != nil {
		t.Errorf("expected no units for an unparseable statement, got %d", len(units))
	}
	if _, ok := pickSubquery(nil, 0); ok {
		t.Error("pickSubquery on an empty split reported a unit")
	}
}

// Each button is exactly its keystroke: the toggle changes which buttons are
// on the bar, never what a keystroke means. Both request paths set the same
// two flags, so what executeRun sees is identical whichever fired.
func TestRunSubqueryRequestMatchesTheChord(t *testing.T) {
	byButton := &PlayApp{}
	// What the Run subquery button's click arm sets.
	byButton.requestRun, byButton.requestSubquery = true, true

	byChord := &PlayApp{}
	byChord.applyRunShortcut(false, true)

	if byButton.requestRun != byChord.requestRun ||
		byButton.requestSubquery != byChord.requestSubquery {
		t.Errorf("button set (run=%v sub=%v), chord set (run=%v sub=%v) — they must agree",
			byButton.requestRun, byButton.requestSubquery,
			byChord.requestRun, byChord.requestSubquery)
	}
	// …and the plain chord never asks for a subquery.
	plain := &PlayApp{}
	plain.applyRunShortcut(true, false)
	if !plain.requestRun || plain.requestSubquery {
		t.Errorf("plain Ctrl+Enter set (run=%v sub=%v), want (true false)",
			plain.requestRun, plain.requestSubquery)
	}
	// Neither fires without a press.
	idle := &PlayApp{}
	idle.applyRunShortcut(false, false)
	if idle.requestRun || idle.requestSubquery {
		t.Error("no press must request no run")
	}
}

// The chord is process-global: every open play instance's poll sees the same
// press. Only the instance in the shell's active window may act on it — the
// unfocused sibling dropping the press IS the fix for one Ctrl+Enter running
// a query in every open playground.
func TestChordClaimRespectsWindowFocus(t *testing.T) {
	unfocused := debouncedApp(t, "SELECT 1")
	unfocused.windowUnfocused = true
	unfocused.claimRunChord(true, false)
	if unfocused.requestRun || unfocused.requestSubquery {
		t.Error("an unfocused instance must drop the press")
	}
	unfocused.claimRunChord(false, true)
	if unfocused.requestRun || unfocused.requestSubquery {
		t.Error("an unfocused instance must drop the narrowed press too")
	}

	// The zero value is focused: a host without the capability (tests,
	// single-surface hosts) keeps today's behavior.
	focused := debouncedApp(t, "SELECT 1")
	if focused.windowUnfocused {
		t.Fatal("focus must be the zero-value default")
	}
	focused.claimRunChord(false, true)
	if !focused.requestRun || !focused.requestSubquery {
		t.Error("the focused instance must claim the press")
	}
}

// The statement's own query closes over its WITH items too — it is just that
// they already travel with it. It gets the environment underlines and no
// background, so a buffer whose WITH clause is most of its length says which
// definitions the query at the bottom rests on.
func TestSubqueryModeMarksTheRootQuerysClosure(t *testing.T) {
	const sql = "WITH\n  ['a', 'b'] AS lifecycle,\n  sub AS (SELECT 1 AS x)\nSELECT x FROM sub ORDER BY indexOf(lifecycle, 'a')"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.subqueryMode = true
	app.caretByte = len("WITH\n  ['a', 'b'] AS lifecycle,\n  sub AS (SELECT 1 AS x)\nSELECT x FRO")

	var carried, tinted []string
	for _, s := range app.editorStyledSections() {
		switch {
		case s.Flags&codeview.StyleBackground != 0:
			tinted = append(tinted, sql[s.Start:s.Stop])
		case s.Flags&codeview.StyleUnderline != 0 && s.Color == styleCarriedTone:
			carried = append(carried, sql[s.Start:s.Stop])
		}
	}
	// The main query IS tinted: the WITH clause sits outside it, so it is a
	// proper subset of the statement and the tint distinguishes the two.
	wantTint := "SELECT x FROM sub ORDER BY indexOf(lifecycle, 'a')"
	if strings.Join(tinted, "|") != wantTint {
		t.Errorf("tinted = %v, want just the main query", tinted)
	}
	want := []string{"['a', 'b'] AS lifecycle", "sub AS (SELECT 1 AS x)"}
	if strings.Join(carried, "|") != strings.Join(want, "|") {
		t.Errorf("closure = %v, want %v", carried, want)
	}
	// Still not a narrowing: the gesture degrades, and the gutter stays quiet.
	// The tint must not leak a mark into it — `>` means the caret's statement,
	// and this is a single-statement buffer with no statement tint at all.
	if _, scope := app.runSubqueryBuffer(); scope != runScopeNoSubquery {
		t.Errorf("scope = %v, want runScopeNoSubquery", scope)
	}
	if r := app.caretSubqueryRange(); !r.Empty() {
		t.Errorf("gutter range = %v, want empty at statement level", r)
	}
	m := app.buildGutterModel(sql, app.editorStyledSections(), app.caretSubqueryRange())
	for i, mark := range m.marks {
		if mark != gutterMarkNone {
			t.Errorf("line %d mark = %v, want none — the query tint is not a statement mark", i+1, mark)
		}
	}
}

// A query that IS the whole statement has nothing outside it to be
// distinguished from, and no closure to draw — so the plain buffer stays
// undecorated even with the mode on. A full-width wash would say nothing.
func TestSubqueryModeQuietWhenTheQueryIsTheStatement(t *testing.T) {
	const sql = "SELECT 1 AS a"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.subqueryMode = true
	app.caretByte = len("SELECT 1")
	if secs := app.editorStyledSections(); len(secs) != 0 {
		t.Errorf("got %d sections, want none", len(secs))
	}
}

// The Subquery toggle is a DISPLAY switch. It must not change what a run
// ships, and with it off the editor must look exactly as it did before the
// feature existed.
func TestSubqueryModeSectionsAreDisplayOnly(t *testing.T) {
	const sql = "WITH t AS (SELECT 1 AS a) SELECT * FROM (SELECT a FROM t)"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.caretByte = len("WITH t AS (SELECT 1 AS a) SELECT * FROM (SELECT a")

	off := app.editorStyledSections()
	offRun, offScope := app.runSubqueryBuffer()
	app.subqueryMode = true
	on := app.editorStyledSections()
	onRun, onScope := app.runSubqueryBuffer()

	if len(on) <= len(off) {
		t.Errorf("mode on produced %d sections, off produced %d — expected more", len(on), len(off))
	}
	if offRun != onRun || offScope != onScope {
		t.Errorf("the toggle changed what ships: %q/%v vs %q/%v", offRun, offScope, onRun, onScope)
	}
	// The subquery's own tint is the only background this buffer has: it is a
	// single statement, so the multi-statement tint is not in play.
	var backgrounds int
	for _, s := range on {
		if s.Flags&codeview.StyleBackground != 0 {
			backgrounds++
			if got := sql[s.Start:s.Stop]; got != "SELECT a FROM t" {
				t.Errorf("tinted %q, want the subquery", got)
			}
		}
	}
	if backgrounds != 1 {
		t.Errorf("got %d background sections, want 1", backgrounds)
	}
	// The carried environment is underlined, not tinted: the WITH item that
	// travels with the narrowed run.
	var carried []string
	for _, s := range on {
		if s.Flags&codeview.StyleUnderline != 0 && s.Color == styleCarriedTone {
			carried = append(carried, sql[s.Start:s.Stop])
		}
	}
	if strings.Join(carried, "|") != "t AS (SELECT 1 AS a)" {
		t.Errorf("carried = %v, want the hoisted WITH item", carried)
	}
}

// The gutter mark is the affordance that survives the toggle being off, so it
// is driven by its own input rather than by the sections.
func TestGutterMarksSubqueryWithModeOff(t *testing.T) {
	const sql = "SELECT * FROM (\n  SELECT 1 AS a\n)"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.caretByte = len("SELECT * FROM (\n  SELECT 1")

	if app.subqueryMode {
		t.Fatal("the toggle must default to off")
	}
	styled := app.editorStyledSections()
	m := app.buildGutterModel(sql, styled, app.caretSubqueryRange())
	if m.marks[1] != gutterMarkSubquery {
		t.Errorf("line 2 mark = %v, want gutterMarkSubquery", m.marks[1])
	}
	// …and it really is coming from the range, not from a section: with the
	// toggle off this buffer produces no background at all.
	for _, s := range styled {
		if s.Flags&codeview.StyleBackground != 0 {
			t.Errorf("mode off emitted a background section over %q", sql[s.Start:s.Stop])
		}
	}
}

// A caret that has never been placed reads as offset 0, which is a real
// position at the head of the buffer — so it resolves to the statement's own
// query and the gesture degrades. That is the shape that made the feature look
// broken; the scope report is what makes it visible.
func TestSubqueryUnplacedCaretDegradesAudibly(t *testing.T) {
	const sql = "SELECT n FROM (SELECT number AS n FROM numbers(5)) WHERE n > 1"
	app := debouncedApp(t, sql)
	app.updatePreview()
	// caretByte left at its zero value, as on a restored buffer never clicked.

	run, scope := app.runSubqueryBuffer()
	if run != sql {
		t.Errorf("run = %q, want the whole query", run)
	}
	if scope != runScopeNoSubquery {
		t.Errorf("scope = %v, want runScopeNoSubquery", scope)
	}
	if note := runScopeNote(scope); note == "" {
		t.Error("the degrade must say so in the status line")
	}
	if r := app.caretSubqueryRange(); !r.Empty() {
		t.Errorf("nothing should be marked, got %v", r)
	}
}

// Run-subquery over the app's own buffer: the prelude rides along, and the
// statement split still picks the caret's statement first.
func TestRunSubqueryBuffer(t *testing.T) {
	cases := []struct {
		name  string
		sql   string
		caret int
		want  string
		scope runScopeE
	}{{
		name:  "prelude rides along with the narrowed query",
		sql:   "SET param_n = 3;\nSELECT * FROM (SELECT number FROM numbers({n:UInt8}))",
		caret: len("SET param_n = 3;\nSELECT * FROM (SELECT num"),
		want:  "SET param_n = 3;\nSELECT number FROM numbers({n:UInt8})",
		scope: runScopeSubquery,
	}, {
		name:  "a multi-statement buffer narrows within the caret's statement",
		sql:   "SELECT 1;\nSELECT * FROM (SELECT 2 AS b)",
		caret: len("SELECT 1;\nSELECT * FROM (SELECT 2"),
		want:  "SELECT 2 AS b",
		scope: runScopeSubquery,
	}, {
		name:  "at statement level it degrades to the ordinary run buffer",
		sql:   "SELECT 1;\nSELECT * FROM (SELECT 2 AS b)",
		caret: len("SELECT 1;\nSELECT *"),
		want:  "SELECT * FROM (SELECT 2 AS b)",
		scope: runScopeNoSubquery,
	}, {
		name:  "an unparseable statement degrades to the ordinary run buffer",
		sql:   "SELECT * FROM (",
		caret: len("SELECT * FROM ("),
		want:  "SELECT * FROM (",
		scope: runScopeNoSubquery,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &PlayApp{sql: tc.sql, caretByte: tc.caret}
			run, scope := app.runSubqueryBuffer()
			if run != tc.want {
				t.Errorf("run:\n got %q\nwant %q", run, tc.want)
			}
			if scope != tc.scope {
				t.Errorf("scope = %v, want %v", scope, tc.scope)
			}
		})
	}
}
