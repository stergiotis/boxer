package sqleditor

// The line-number gutter's pure model (ADR-0130 L3). Rendering is verified
// live; what is asserted here is the line arithmetic the alignment rests on,
// and the rule that a mark is earned by a section's TONE — which is what keeps
// the marks lane and the text decoration from ever disagreeing.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stretchr/testify/require"
)

func TestLineStarts(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want []int
	}{
		{"single line", "SELECT 1", []int{0}},
		{"two lines", "SELECT 1\nFROM t", []int{0, 9}},
		// A trailing newline must not manufacture a phantom final line — the
		// editor's galley does not draw one either, and a mismatch puts every
		// mark below it one row off.
		{"trailing newline", "SELECT 1\n", []int{0}},
		{"blank line between", "a\n\nb", []int{0, 2, 3}},
		{"empty", "", []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, lineStarts(tc.s))
		})
	}
}

func TestLineIndexOf(t *testing.T) {
	starts := lineStarts("SELECT 1\nFROM t\nWHERE x")
	require.Equal(t, []int{0, 9, 16}, starts)
	require.Equal(t, 0, lineIndexOf(starts, 0))
	require.Equal(t, 0, lineIndexOf(starts, 8))
	require.Equal(t, 1, lineIndexOf(starts, 9))
	require.Equal(t, 1, lineIndexOf(starts, 15))
	require.Equal(t, 2, lineIndexOf(starts, 16))
	require.Equal(t, 2, lineIndexOf(starts, 999), "past the end clamps to the last line")
	require.Equal(t, 0, lineIndexOf(starts, -5), "before the start clamps to the first")
}

// Numbers are right-aligned in a fixed-width column, so the ones digit stays
// in the same monospace cell from line 1 to line 100.
func TestGutterTextRightAlignsNumbers(t *testing.T) {
	m := gutterModel{lines: 3, marks: make([]gutterMarkE, 3), digits: 3, present: true}
	text, spans, rest := m.gutterText()
	lines := strings.Split(text, "\n")
	require.Equal(t, []string{"   1", "   2", "   3"}, lines)
	require.Len(t, spans, 3)
	// Each mark span is the single leading cell of its line.
	for i, sp := range spans {
		require.Equal(t, 1, sp[1]-sp[0])
		require.Equal(t, " ", text[sp[0]:sp[1]], "line %d has no mark", i+1)
	}
	// Together the two span families must claim EVERY byte, contiguously: a
	// CodeViewJob does not gap-fill, so an unclaimed byte loses its glyph.
	cursor := 0
	for i := range spans {
		require.Equal(t, cursor, spans[i][0], "gap before the mark on line %d", i+1)
		require.Equal(t, spans[i][1], rest[i][0], "gap after the mark on line %d", i+1)
		cursor = rest[i][1]
	}
	require.Equal(t, len(text), cursor, "coverage must reach the end")
}

func TestGutterTextMarks(t *testing.T) {
	m := gutterModel{
		lines:   3,
		marks:   []gutterMarkE{gutterMarkNone, gutterMarkActive, gutterMarkError},
		digits:  1,
		present: true,
	}
	text, spans, _ := m.gutterText()
	require.Equal(t, []string{" 1", ">2", "!3"}, strings.Split(text, "\n"))
	require.Equal(t, ">", text[spans[1][0]:spans[1][1]])
	require.Equal(t, "!", text[spans[2][0]:spans[2][1]])
}

func underline(start, stop uint32, tone codeview.StyledSection) codeview.StyledSection {
	tone.Start, tone.Stop, tone.Flags = start, stop, codeview.StyleUnderline
	return tone
}

// The marks come from the same overlays that decorate the text, recognised by
// tone, so the two can never disagree about which line is which.
func TestGutterModelMarksFollowTheOverlays(t *testing.T) {
	const sql = "SELECT 1;\nSELCT 2;\nSELECT 3"
	tint := codeview.StyledSection{
		Start: 0, Stop: 8, Flags: codeview.StyleBackground, Color: ToneStatementTint,
	}
	m := buildGutterModel(sql, []codeview.StyledSection{tint}, nanopass.SourceRange{}, 0)
	require.True(t, m.present)
	require.Equal(t, 3, m.lines)
	require.Equal(t, 1, m.digits)
	require.Equal(t, []gutterMarkE{gutterMarkActive, gutterMarkNone, gutterMarkNone}, m.marks)

	// An error underline on the second line takes the error mark, which
	// outranks the active mark on the same line.
	tint.Start, tint.Stop = 10, 17
	err := underline(10, 15, codeview.StyledSection{Color: ToneError})
	m = buildGutterModel(sql, []codeview.StyledSection{tint, err}, nanopass.SourceRange{}, 0)
	require.Equal(t, []gutterMarkE{gutterMarkNone, gutterMarkError, gutterMarkNone}, m.marks)
}

// A background in some other tone is not the statement tint, and must not earn
// the active mark — the subquery tint is a background too, and means something
// the `>` mark does not.
func TestGutterModelIgnoresForeignTones(t *testing.T) {
	const sql = "SELECT 1\nFROM t"
	subqTint := codeview.StyledSection{
		Start: 0, Stop: 8, Flags: codeview.StyleBackground, Color: ToneSubqueryTint,
	}
	warn := underline(9, 14, codeview.StyledSection{Color: ToneWarning})
	m := buildGutterModel(sql, []codeview.StyledSection{subqTint, warn}, nanopass.SourceRange{}, 0)
	require.Equal(t, []gutterMarkE{gutterMarkNone, gutterMarkNone}, m.marks)
}

// The subquery mark arrives on its own channel, because it is drawn whether or
// not the embedder tinted it, and it outranks the active mark.
func TestGutterModelSubqueryMark(t *testing.T) {
	const sql = "SELECT * FROM (\n  SELECT 1\n) t"
	tint := codeview.StyledSection{
		Start: 0, Stop: uint32(len(sql)), Flags: codeview.StyleBackground, Color: ToneStatementTint,
	}
	m := buildGutterModel(sql, []codeview.StyledSection{tint},
		nanopass.SourceRange{Start: 18, End: 26}, 0)
	require.Equal(t, []gutterMarkE{gutterMarkActive, gutterMarkSubquery, gutterMarkActive}, m.marks)
}

// A multi-line statement marks every line it spans.
func TestGutterModelMarksSpanMultipleLines(t *testing.T) {
	const sql = "SELECT 1;\nSELECT 2\nFROM t\nWHERE x = 1"
	tint := codeview.StyledSection{
		Start: 10, Stop: uint32(len(sql)), Flags: codeview.StyleBackground, Color: ToneStatementTint,
	}
	m := buildGutterModel(sql, []codeview.StyledSection{tint}, nanopass.SourceRange{}, 0)
	require.Equal(t, []gutterMarkE{
		gutterMarkNone, gutterMarkActive, gutterMarkActive, gutterMarkActive,
	}, m.marks)
}

func TestGutterModelEmptyBuffer(t *testing.T) {
	require.False(t, buildGutterModel("", nil, nanopass.SourceRange{}, 0).present)
}

// The digit width grows with the line count, so a 100-line buffer reserves
// three columns and the numbers stay in step.
func TestGutterDigitsGrowWithLineCount(t *testing.T) {
	for _, tc := range []struct{ lines, digits int }{{1, 1}, {9, 1}, {10, 2}, {99, 2}, {100, 3}} {
		buf := strings.TrimSuffix(strings.Repeat("SELECT 1\n", tc.lines), "\n")
		m := buildGutterModel(buf, nil, nanopass.SourceRange{}, 0)
		require.Equal(t, tc.lines, m.lines)
		require.Equal(t, tc.digits, m.digits, "%d lines", tc.lines)
	}
}

// The editor's desired width has to cover the longest line, or no-wrap layout
// clips its tail out of reach.
func TestEditorWidthPx(t *testing.T) {
	const charPx float32 = 8
	// 20-char longest line + trailing slack, well past the pane.
	got := editorWidthPx("SELECT 1\n"+strings.Repeat("x", 20), charPx, 50)
	require.InDelta(t, float32(20+editorTrailingCols)*charPx, got, 0.01)
	// A short buffer never shrinks the editor below the pane.
	require.InDelta(t, 400, editorWidthPx("SELECT 1", charPx, 400), 0.01)
	// Runes, not bytes: a multibyte line must not over-reserve.
	require.InDelta(t,
		editorWidthPx(strings.Repeat("x", 5), charPx, 0),
		editorWidthPx(strings.Repeat("€", 5), charPx, 0), 0.01)
}

// The gutter and the editor are handed ONE overlay list in one coordinate
// system. Behind a hide-prelude toggle the widget rebases it once; the gutter
// must not subtract the elided prefix a second time, and a span the rebase
// dropped must not reappear as a mark.
func TestGutterModelTakesTheEditorsOwnSections(t *testing.T) {
	const prelude = "SET param_a = 1;\n"
	const mirror = "SELECT 1;\nSELECT 2"
	// The second statement's tint, in canonical coordinates.
	tint := codeview.StyledSection{
		Start: uint32(len(prelude) + 10), Stop: uint32(len(prelude) + len(mirror)),
		Flags: codeview.StyleBackground, Color: ToneStatementTint,
	}
	rebased := ShiftStyledSections([]codeview.StyledSection{tint}, len(prelude), len(mirror))
	m := buildGutterModel(mirror, rebased, nanopass.SourceRange{}, 0)
	require.Equal(t, 2, m.lines)
	require.Equal(t, []gutterMarkE{gutterMarkNone, gutterMarkActive}, m.marks,
		"the mark lands on the mirror's own line 2, not shifted twice")

	// A span lying entirely inside the elided prelude is dropped by the
	// rebase, so it can never mark a mirror line.
	inPrelude := []codeview.StyledSection{underline(4, 9, codeview.StyledSection{Color: ToneError})}
	require.Empty(t, ShiftStyledSections(inPrelude, len(prelude), len(mirror)))
	m = buildGutterModel(mirror, ShiftStyledSections(inPrelude, len(prelude), len(mirror)),
		nanopass.SourceRange{}, 0)
	require.Equal(t, []gutterMarkE{gutterMarkNone, gutterMarkNone}, m.marks)
}
