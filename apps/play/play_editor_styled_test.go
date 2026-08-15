package play

// The editor's styled-overlay producers (ADR-0130 L3): the syntax-error
// underline's position mapping and token lookup, and the quiescence gate
// every overlay is behind.

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stretchr/testify/require"
)

// The underline must land on a real token, whatever the parser pointed at.
func TestErrorTokenSpanCoversARealToken(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string // the exact text the underline should cover
	}{
		{"misspelled leading keyword", "SELCT 1", "SELCT"},
		{"garbage word", "SELECT 1 FROM t WHERE x ?? 2", "?"},
		{"multi-line", "SELECT 1\nFROM t\nWHERE ,", ","},
		{"unterminated tail", "SELECT 1 FROM", "FROM"},
		// An unterminated literal lexes as a bare quote token; the underline
		// sits on it rather than running to the end of the buffer.
		{"unterminated string literal", "SELECT 'unterminated", "'"},
		// An EOF-positioned error resolves back to the last real token.
		{"truncated clause", "SELECT 1 FROM t GROUP", "GROUP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := firstSyntaxError(tc.sql)
			require.True(t, pos.Ok, "test input must not parse: %q", tc.sql)
			start, stop, ok := errorTokenSpan(tc.sql, pos)
			require.True(t, ok, "an unparseable buffer must yield a span")
			require.LessOrEqual(t, int(stop), len(tc.sql))
			require.Less(t, start, stop, "span must be non-empty")
			got := tc.sql[start:stop]
			require.Equal(t, tc.want, got)
			require.NotEqual(t, "", strings.TrimSpace(got),
				"the underline must never sit on whitespace alone")
		})
	}
}

// A position at EOF has no covering token; the last real one takes it.
func TestErrorTokenSpanAtEOF(t *testing.T) {
	sql := "SELECT 1 FROM t WHERE "
	start, stop, ok := errorTokenSpan(sql, syntaxErrorPos{Line: 1, Column: 99, Ok: true})
	require.True(t, ok)
	require.Equal(t, "WHERE", sql[start:stop])
}

// A clean buffer, an empty buffer, and a non-error position produce nothing.
func TestErrorTokenSpanDegenerate(t *testing.T) {
	_, _, ok := errorTokenSpan("SELECT 1", syntaxErrorPos{})
	require.False(t, ok, "no error ⇒ no span")
	_, _, ok = errorTokenSpan("", syntaxErrorPos{Line: 1, Ok: true})
	require.False(t, ok, "empty buffer ⇒ no span")
	_, _, ok = errorTokenSpan("   \n  ", syntaxErrorPos{Line: 1, Ok: true})
	require.False(t, ok, "whitespace-only buffer has no real token")
}

// End to end through the app: a broken buffer underlines, a fixed one clears.
func TestEditorStyledSectionsErrorUnderline(t *testing.T) {
	app := debouncedApp(t, "SELCT 1")
	app.updatePreview()
	require.Error(t, app.formattedErr)

	secs := app.editorStyledSections()
	require.Len(t, secs, 1)
	require.Equal(t, codeview.StyleUnderline, secs[0].Flags)
	require.Equal(t, "SELCT", app.sql[secs[0].Start:secs[0].Stop])

	// Fix it: the pipeline reruns and the underline goes.
	app.sql = "SELECT 1"
	app.lastSeenSql = app.sql
	app.lastEditAt = time.Now().Add(-2 * previewDebounce)
	app.updatePreview()
	require.NoError(t, app.formattedErr)
	require.Empty(t, app.editorStyledSections())
}

// While the user is typing, the recorded span describes the previous buffer;
// the quiescence gate suppresses every overlay rather than showing a stale one.
func TestEditorStyledSectionsGatedOnQuiescence(t *testing.T) {
	app := debouncedApp(t, "SELCT 1")
	app.updatePreview()
	require.Len(t, app.editorStyledSections(), 1)

	app.sql = "SELCT 12" // typed since; the debounce has not caught up
	require.Empty(t, app.editorStyledSections(),
		"a buffer the pipeline has not seen carries no overlays")
}

// A no-op app (bare construction, no pipeline run) must not panic or produce
// spans — several unit tests build a PlayApp this way for unrelated work.
func TestEditorStyledSectionsOnBareApp(t *testing.T) {
	require.Empty(t, (&PlayApp{}).editorStyledSections())
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "", nil)
	require.Empty(t, app.editorStyledSections())
}

// --- multi-statement composition (M3 × M5) ---

// A multi-statement buffer never parses whole (grammar1's QueryStmt is
// single-statement), so the underline must come from the caret's statement —
// otherwise every such buffer is flagged as broken at the boundary between two
// perfectly good statements.
//
// The active-statement TINT is no longer play's (ADR-0147 §SD2 gave it to the
// widget, which derives it from buffer and caret), so what these assert is
// play's own contribution: the error underline and nothing else.
func TestMultiStatementErrorUnderlineScopesToTheCaretsStatement(t *testing.T) {
	const sql = "SELECT 1; SELCT 2"
	app := debouncedApp(t, sql)
	app.updatePreview()
	require.Error(t, app.formattedErr, "the whole buffer does not parse…")

	// Caret in the healthy statement: nothing at all.
	app.caretByte = 3
	require.Empty(t, app.editorStyledSections(),
		"…yet the healthy statement carries no error")

	// Caret in the broken one: an underline on its bad token.
	app.caretByte = len(sql)
	secs := app.editorStyledSections()
	require.Len(t, secs, 1)
	require.Equal(t, codeview.StyleUnderline, secs[0].Flags)
	require.Equal(t, "SELCT", sql[secs[0].Start:secs[0].Stop],
		"the underline sits on the offending token, in buffer coordinates")

	// The tint the widget will draw covers the caret's statement, so the
	// underline lands inside it — the property the two used to share by being
	// built together, now asserted across the seam.
	stmt, _, total, ok := app.caretStatement()
	require.True(t, ok)
	require.Equal(t, 2, total)
	require.GreaterOrEqual(t, int(secs[0].Start), stmt.Src.Start)
	require.LessOrEqual(t, int(secs[0].Stop), stmt.Src.End)
}

// Two healthy statements: play contributes nothing, and the widget's tint is
// what marks the active one.
func TestMultiStatementHealthyBufferTintsOnly(t *testing.T) {
	const sql = "SELECT 1; SELECT 2"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.caretByte = 3
	require.Empty(t, app.editorStyledSections())
	stmt, _, total, ok := app.caretStatement()
	require.True(t, ok)
	require.Equal(t, 2, total, "…but the buffer IS multi-statement")
	require.Equal(t, "SELECT 1", sql[stmt.Src.Start:stmt.Src.End])
}

// A single-statement buffer is visually unchanged: no tint at all.
func TestSingleStatementBufferHasNoTint(t *testing.T) {
	app := debouncedApp(t, "SELECT 1")
	app.updatePreview()
	require.Empty(t, app.editorStyledSections())
	_, _, total, _ := app.caretStatement()
	require.Equal(t, 1, total, "the widget's tint is gated on total > 1")
}

// The per-statement parse is memoised on the statement text, so caret travel
// inside one statement does not re-parse.
func TestStatementSyntaxErrorMemo(t *testing.T) {
	app := &PlayApp{}
	first := app.statementSyntaxError("SELCT 1")
	require.True(t, first.Ok)
	require.Equal(t, "SELCT 1", app.stmtErrFor)
	require.Equal(t, first, app.statementSyntaxError("SELCT 1"))
	// A different statement replaces the entry.
	second := app.statementSyntaxError("SELECT 2")
	require.False(t, second.Ok)
	require.Equal(t, "SELECT 2", app.stmtErrFor)
}

// --- unfilled placeholders (ADR-0124 §SD8, M8) ---

// The underline is driven by the same set the Run gate reads, so the editor
// and the gate cannot disagree about what is blocking a Run.
func TestUnfilledPlaceholderUnderline(t *testing.T) {
	const sql = "SELECT * FROM t WHERE a = {a:UInt64} AND b = {b:String}"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.frameSig = app.graph.signals()

	require.ElementsMatch(t, []string{"a", "b"}, app.unfilledInputs())
	secs := app.editorStyledSections()
	require.Len(t, secs, 2)
	covered := []string{}
	for _, s := range secs {
		require.Equal(t, codeview.StyleUnderline, s.Flags)
		require.Equal(t, styleWarningTone, s.Color)
		covered = append(covered, sql[s.Start:s.Stop])
	}
	require.ElementsMatch(t, []string{"{a:UInt64}", "{b:String}"}, covered)
}

// Filling one retires its underline and leaves the other's — the same
// per-name granularity the Run gate has.
func TestUnfilledUnderlineRetiresWithTheRunGate(t *testing.T) {
	const sql = "SET param_a = 1;\nSELECT * FROM t WHERE a = {a:UInt64} AND b = {b:String}"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.frameSig = app.graph.signals()

	require.Equal(t, []string{"b"}, app.unfilledInputs(), "the SET binds a")
	secs := app.editorStyledSections()
	require.Len(t, secs, 1)
	require.Equal(t, "{b:String}", sql[secs[0].Start:secs[0].Stop])
}

// Nothing unfilled ⇒ nothing underlined.
func TestNoUnfilledPlaceholdersNoUnderline(t *testing.T) {
	const sql = "SET param_a = 1;\nSELECT {a:UInt64}"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.frameSig = app.graph.signals()
	require.Empty(t, app.unfilledInputs())
	require.Empty(t, app.editorStyledSections())
}

// --- caret-marked parameter row ---

// The pane marks the row holding the placeholder the caret is in, from the
// same slot Src the underline uses.
func TestCaretOnClaim(t *testing.T) {
	//                       0         1         2         3         4
	//                       0123456789012345678901234567890123456789012
	const sql = "SELECT * FROM t WHERE a = {a:UInt64} AND b = {b:String}"
	app := debouncedApp(t, sql)
	app.updatePreview()
	require.Len(t, app.paramSlots, 2)
	a, b := app.paramSlots[0], app.paramSlots[1]
	require.Equal(t, "{a:UInt64}", sql[a.Src.Start:a.Src.End])
	require.Equal(t, "{b:String}", sql[b.Src.Start:b.Src.End])

	cases := []struct {
		name  string
		caret int
		onA   bool
		onB   bool
	}{
		{"before any slot", 5, false, false},
		{"on the opening brace of a", a.Src.Start, true, false},
		{"inside a", a.Src.Start + 3, true, false},
		{"just past a's closing brace", a.Src.End, true, false},
		{"in the gap between them", a.Src.End + 3, false, false},
		{"inside b", b.Src.Start + 2, false, true},
		{"at the end of the buffer", len(sql), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app.caretByte = tc.caret
			require.Equal(t, tc.onA, app.caretOnClaim([]paramSlot{a}))
			require.Equal(t, tc.onB, app.caretOnClaim([]paramSlot{b}))
			// A folded pair claims both slots, so it marks for either.
			require.Equal(t, tc.onA || tc.onB, app.caretOnClaim([]paramSlot{a, b}))
		})
	}
}

// Mid-edit the slot ranges describe the previous buffer, so no row is marked
// rather than the wrong one — the same quiescence gate the overlays use.
func TestCaretOnClaimGatedOnQuiescence(t *testing.T) {
	const sql = "SELECT * FROM t WHERE a = {a:UInt64}"
	app := debouncedApp(t, sql)
	app.updatePreview()
	slot := app.paramSlots[0]
	app.caretByte = slot.Src.Start + 2
	require.True(t, app.caretOnClaim([]paramSlot{slot}))

	app.sql = "SELECT 1 " + sql // typed since; the debounce has not caught up
	require.False(t, app.caretOnClaim([]paramSlot{slot}))
}

// Degenerate inputs mark nothing rather than panicking — a bare app, an empty
// range, and a range past the end of the buffer it is sliced against.
func TestCaretOnClaimDegenerate(t *testing.T) {
	require.False(t, (&PlayApp{}).caretOnClaim([]paramSlot{{Name: "a"}}))

	app := debouncedApp(t, "SELECT {a:UInt64}")
	app.updatePreview()
	require.False(t, app.caretOnClaim(nil))
	require.False(t, app.caretOnClaim([]paramSlot{{Name: "x"}}), "empty Src")
	require.False(t, app.caretOnClaim([]paramSlot{{
		Name: "x", Src: nanopass.SourceRange{Start: 0, End: len(app.sql) + 99},
	}}), "a range past the buffer end")
}
