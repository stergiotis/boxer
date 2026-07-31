package sqleditor

// The offset arithmetic the caret channel and the overlay rebasing rest on
// (ADR-0130 L3), plus the caret lift [Editor.Bind] performs.

import (
	"testing"

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
			require.Equal(t, tc.want, ByteOffsetOfLineCol(tc.sql, tc.line, tc.col))
		})
	}
}

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
			require.Equal(t, tc.want, ByteOffsetOfChar(tc.s, tc.chars))
		})
	}
}

// A position at EOF has no covering token; the last real one takes it.
func TestErrorTokenSpanAtEOF(t *testing.T) {
	sql := "SELECT 1 FROM t WHERE "
	start, stop, ok := ErrorTokenSpan(sql, 1, 99)
	require.True(t, ok)
	require.Equal(t, "WHERE", sql[start:stop])
}

// An empty buffer and a whitespace-only one produce nothing.
func TestErrorTokenSpanDegenerate(t *testing.T) {
	_, _, ok := ErrorTokenSpan("", 1, 0)
	require.False(t, ok, "empty buffer ⇒ no span")
	_, _, ok = ErrorTokenSpan("   \n  ", 1, 0)
	require.False(t, ok, "whitespace-only buffer has no real token")
}

func TestShiftStyledSections(t *testing.T) {
	secs := []codeview.StyledSection{
		{Start: 2, Stop: 5, Flags: codeview.StyleUnderline},    // inside the prelude
		{Start: 8, Stop: 14, Flags: codeview.StyleUnderline},   // straddles
		{Start: 20, Stop: 24, Flags: codeview.StyleBackground}, // fully visible
	}
	// prelude is 10 bytes, the visible view is 20 bytes
	got := ShiftStyledSections(secs, 10, 20)
	require.Len(t, got, 2)
	require.Equal(t, uint32(0), got[0].Start, "the straddling span trims to the view start")
	require.Equal(t, uint32(4), got[0].Stop)
	require.Equal(t, uint32(10), got[1].Start)
	require.Equal(t, uint32(14), got[1].Stop)

	// A zero offset is the identity.
	require.Equal(t, secs, ShiftStyledSections(secs, 0, 24))
	// Everything past the view end drops.
	require.Empty(t, ShiftStyledSections(secs, 30, 20))
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

// Bind converts the reported CHAR caret to a byte offset, and lifts it out of
// a bound suffix view into canonical coordinates.
func TestBindLiftsTheCaretIntoCanonicalCoordinates(t *testing.T) {
	buf := "SELECT '€' FROM t"
	ed := New()
	// caret after the multibyte char: char 9 → byte 11
	ed.caretPacked = uint64(9) | uint64(9)<<32
	res := ed.Bind(Frame{IDSlot: "e", Value: &buf})
	require.Equal(t, 11, res.Caret)
	require.Equal(t, buf, res.Buffer)

	// A suffix view reports offsets into itself; Bind puts them back into the
	// canonical buffer's coordinates. This is resolved ONCE, against the view
	// that actually rendered — resolving against the canonical buffer would
	// come up short by exactly the elided prelude.
	const prelude = "SET param_a = 1;\n"
	mirror := "SELECT 1"
	ed = New()
	ed.caretPacked = uint64(3) | uint64(3)<<32
	res = ed.Bind(Frame{
		IDSlot: "e", Value: &mirror, Offset: len(prelude), Canonical: prelude + mirror,
	})
	require.Equal(t, len(prelude)+3, res.Caret)
	require.Equal(t, prelude+mirror, res.Buffer)
	require.Equal(t, byte('L'), res.Buffer[res.Caret-1], "caret sits just past SEL")
}

// A nil buffer is a no-op rather than a panic: an embedder may mount the
// editor before it has anything to bind.
func TestBindWithoutABuffer(t *testing.T) {
	ed := New()
	require.Equal(t, Result{}, ed.Bind(Frame{IDSlot: "e"}))
	require.Equal(t, Result{}, ed.Result())
}

// Bind publishes what the caret points at, so a consumer reads it rather than
// re-deriving it from the buffer and a caret of its own (ADR-0147 §SD2).
func TestBindPublishesTheCaretEntity(t *testing.T) {
	buf := "SELECT toHour(now()) FROM t"
	ed := New()
	// Caret inside `toHour`, char == byte here.
	ed.caretPacked = uint64(9) | uint64(9)<<32
	res := ed.Bind(Frame{IDSlot: "e", Value: &buf})
	require.True(t, res.EntityOk)
	require.Equal(t, "toHour", res.Entity.Name)
	require.True(t, res.Entity.Call)
	require.Equal(t, "toHour", res.Buffer[res.Entity.Start:res.Entity.Stop])

	// Inside the argument list: no name of its own, but the call encloses it.
	ed.caretPacked = uint64(14) | uint64(14)<<32
	res = ed.Bind(Frame{IDSlot: "e", Value: &buf})
	require.Equal(t, []string{"toHour"}, res.Entity.Enclosing)

	// On a literal: nothing to report, and that is not an error.
	lit := "SELECT 123"
	ed.caretPacked = uint64(8) | uint64(8)<<32
	res = ed.Bind(Frame{IDSlot: "e", Value: &lit})
	require.False(t, res.EntityOk)
}

// A suffix view resolves the entity in canonical coordinates too, or a
// consumer slicing Buffer by the reported range would read the wrong bytes.
func TestBindEntityUsesCanonicalCoordinates(t *testing.T) {
	const prelude = "SET param_a = 1;\n"
	mirror := "SELECT toHour(x)"
	ed := New()
	ed.caretPacked = uint64(9) | uint64(9)<<32 // inside toHour, mirror coordinates
	res := ed.Bind(Frame{
		IDSlot: "e", Value: &mirror, Offset: len(prelude), Canonical: prelude + mirror,
	})
	require.True(t, res.EntityOk)
	require.Equal(t, "toHour", res.Entity.Name)
	require.Equal(t, "toHour", res.Buffer[res.Entity.Start:res.Entity.Stop])
}
