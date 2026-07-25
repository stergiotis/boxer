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
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stretchr/testify/require"
)

func TestByteOffsetOfLineCol(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		line int
		col  int
		want int
	}{
		{"first line, first column", "SELECT 1", 1, 0, 0},
		{"first line, mid", "SELECT 1", 1, 7, 7},
		{"second line", "SELECT 1\nFROM t", 2, 0, 9},
		{"second line, mid", "SELECT 1\nFROM t", 2, 5, 14},
		{"third line", "a\nb\nc", 3, 0, 4},
		// The column is a RUNE offset: three 3-byte chars ahead of the caret.
		{"multibyte column", "SELECT '€€€' , x", 1, 11, 17},
		{"multibyte second line", "SELECT '€'\nFROM t", 2, 4, 17},
		// Clamping: past the end of the buffer, past the end of a line.
		{"line past end", "SELECT 1", 9, 0, 8},
		{"column past line end", "SELECT 1\nFROM t", 1, 99, 8},
		{"column past buffer end", "SELECT 1", 1, 99, 8},
		{"line zero clamps to one", "SELECT 1", 0, 3, 3},
		{"empty buffer", "", 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, byteOffsetOfLineCol(tc.sql, tc.line, tc.col))
		})
	}
}

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

func TestShiftStyledSections(t *testing.T) {
	secs := []codeview.StyledSection{
		{Start: 2, Stop: 5, Flags: codeview.StyleUnderline},    // inside the prelude
		{Start: 8, Stop: 14, Flags: codeview.StyleUnderline},   // straddles
		{Start: 20, Stop: 24, Flags: codeview.StyleBackground}, // fully visible
	}
	// prelude is 10 bytes, the visible view is 20 bytes
	got := shiftStyledSections(secs, 10, 20)
	require.Len(t, got, 2)
	require.Equal(t, uint32(0), got[0].Start, "the straddling span trims to the view start")
	require.Equal(t, uint32(4), got[0].Stop)
	require.Equal(t, uint32(10), got[1].Start)
	require.Equal(t, uint32(14), got[1].Stop)

	// A zero offset is the identity.
	require.Equal(t, secs, shiftStyledSections(secs, 0, 24))
	// Everything past the view end drops.
	require.Empty(t, shiftStyledSections(secs, 30, 20))
}

// A no-op app (bare construction, no pipeline run) must not panic or produce
// spans — several unit tests build a PlayApp this way for unrelated work.
func TestEditorStyledSectionsOnBareApp(t *testing.T) {
	require.Empty(t, (&PlayApp{}).editorStyledSections())
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	require.Empty(t, app.editorStyledSections())
}

// --- caret channel (ADR-0130 L3, M4) ---

func TestByteOffsetOfChar(t *testing.T) {
	cases := []struct {
		name  string
		s     string
		chars int
		want  int
	}{
		{"ascii start", "SELECT 1", 0, 0},
		{"ascii mid", "SELECT 1", 6, 6},
		{"ascii end", "SELECT 1", 8, 8},
		{"multibyte", "a€b", 2, 4},
		{"multibyte end", "a€b", 3, 5},
		{"newlines are one char", "a\nb", 2, 2},
		// A stale caret from a longer buffer clamps to the end rather than
		// panicking or reading past it.
		{"clamps past end", "SELECT 1", 99, 8},
		{"clamps on empty", "", 5, 0},
		{"negative clamps to zero", "SELECT 1", -3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, byteOffsetOfChar(tc.s, tc.chars))
		})
	}
}

func TestUnpackCursorRangeRoundTrip(t *testing.T) {
	// The Rust side packs low=start, high=end.
	packed := uint64(7) | uint64(11)<<32
	start, end := c.UnpackCursorRange(packed)
	require.Equal(t, 7, start)
	require.Equal(t, 11, end)
	// A collapsed caret reports start == end.
	start, end = c.UnpackCursorRange(uint64(4) | uint64(4)<<32)
	require.Equal(t, start, end)
	// The zero value is a caret at the buffer start.
	start, end = c.UnpackCursorRange(0)
	require.Equal(t, 0, start)
	require.Equal(t, 0, end)
}

func TestRefreshCaretConvertsAndOffsets(t *testing.T) {
	app := &PlayApp{sql: "SELECT '€' FROM t"}
	// caret after the multibyte char: char 9 → byte 11
	app.caretPacked = uint64(9) | uint64(9)<<32
	app.refreshCaret(app.sql, 0)
	require.Equal(t, 11, app.caretByte)

	// The residual mirror reports offsets into its own view; refreshCaret
	// lifts them back into inst.sql coordinates.
	const prelude = "SET param_a = 1;\n"
	mirror := "SELECT 1"
	app = &PlayApp{sql: prelude + mirror}
	app.caretPacked = uint64(3) | uint64(3)<<32
	app.refreshCaret(mirror, len(prelude))
	require.Equal(t, len(prelude)+3, app.caretByte)
	require.Equal(t, byte('L'), app.sql[app.caretByte-1], "caret sits just past SEL")
}

// --- multi-statement composition (M3 × M5) ---

// A multi-statement buffer never parses whole (grammar1's QueryStmt is
// single-statement), so the underline must come from the caret's statement —
// otherwise every such buffer is flagged as broken at the boundary between two
// perfectly good statements.
func TestMultiStatementErrorUnderlineScopesToTheCaretsStatement(t *testing.T) {
	const sql = "SELECT 1; SELCT 2"
	app := debouncedApp(t, sql)
	app.updatePreview()
	require.Error(t, app.formattedErr, "the whole buffer does not parse…")

	// Caret in the healthy statement: tint only, no underline.
	app.caretByte = 3
	secs := app.editorStyledSections()
	require.Len(t, secs, 1, "…yet the healthy statement carries no error")
	require.Equal(t, codeview.StyleBackground, secs[0].Flags)
	require.Equal(t, "SELECT 1", sql[secs[0].Start:secs[0].Stop])

	// Caret in the broken one: tint plus an underline on its bad token.
	app.caretByte = len(sql)
	secs = app.editorStyledSections()
	require.Len(t, secs, 2)
	require.Equal(t, codeview.StyleBackground, secs[0].Flags)
	require.Equal(t, "SELCT 2", sql[secs[0].Start:secs[0].Stop])
	require.Equal(t, codeview.StyleUnderline, secs[1].Flags)
	require.Equal(t, "SELCT", sql[secs[1].Start:secs[1].Stop],
		"the underline sits on the offending token, in buffer coordinates")
}

// Two healthy statements: a tint, and nothing else.
func TestMultiStatementHealthyBufferTintsOnly(t *testing.T) {
	const sql = "SELECT 1; SELECT 2"
	app := debouncedApp(t, sql)
	app.updatePreview()
	app.caretByte = 3
	secs := app.editorStyledSections()
	require.Len(t, secs, 1)
	require.Equal(t, codeview.StyleBackground, secs[0].Flags)
	require.Equal(t, "SELECT 1", sql[secs[0].Start:secs[0].Stop])
}

// A single-statement buffer is visually unchanged: no tint at all.
func TestSingleStatementBufferHasNoTint(t *testing.T) {
	app := debouncedApp(t, "SELECT 1")
	app.updatePreview()
	require.Empty(t, app.editorStyledSections())
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
