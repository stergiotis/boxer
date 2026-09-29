package regexsummary

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// TestTruncatePatternShort exercises the "no truncation needed" branch:
// patterns at or below the cap pass through unchanged.
func TestTruncatePatternShort(t *testing.T) {
	cases := []struct {
		in     string
		maxLen int
		want   string
	}{
		{"", 32, ""},
		{`\w+`, 32, `\w+`},
		{"abc", 3, "abc"},
		{"abcd", 4, "abcd"},
	}
	for _, tc := range cases {
		got := truncatePattern(tc.in, tc.maxLen)
		assert.Equal(t, tc.want, got, "in=%q maxLen=%d", tc.in, tc.maxLen)
	}
}

// TestTruncatePatternLong pins the truncation contract: the displayed
// string is at most maxLen runes long and ends with the ellipsis when
// any cut occurred.
func TestTruncatePatternLong(t *testing.T) {
	got := truncatePattern("abcdefghij", 5)
	assert.Equal(t, "abcd…", got)
	// Ellipsis counts as one rune in the cap.
	runes := []rune(got)
	assert.Len(t, runes, 5)
}

// TestTruncatePatternMultibyte locks the rune-boundary contract: a cut
// must never land mid-codepoint and the output must remain valid UTF-8.
// Tests both Latin Extended (2-byte) and CJK (3-byte) so the byte-vs-
// rune distinction stays exercised.
func TestTruncatePatternMultibyte(t *testing.T) {
	in := "αβγδεζηθικλ" // 11 Greek letters, 2 bytes each in UTF-8
	got := truncatePattern(in, 5)
	gotRunes := []rune(got)
	assert.Len(t, gotRunes, 5)
	assert.Equal(t, "αβγδ…", got)
}

// TestTruncatePatternClamps confirms the documented "n<1 → default"
// contract so a typo at the call site doesn't yield an empty inline
// label.
func TestTruncatePatternClamps(t *testing.T) {
	got := truncatePattern("abcdef", 0)
	// With maxLen clamped to defaultPatternMaxLen (32), 6-char input
	// fits and is returned verbatim.
	assert.Equal(t, "abcdef", got)
	got = truncatePattern("abcdef", -10)
	assert.Equal(t, "abcdef", got)
}

// TestCompileStatusColorEmpty pins the documented "empty pattern →
// elide the dot" contract: ok=false signals the level-1 caller that
// there is no meaningful status to show.
func TestCompileStatusColorEmpty(t *testing.T) {
	_, ok := compileStatusColor("")
	assert.False(t, ok)
}

// TestCompileStatusColorValid + Invalid pin the green/red distinction.
// Two distinct colours must come back so the user can read compile
// validity from the level-1 row at a glance; comparing against the
// styletokens source is the canonical way to spell those colours.
func TestCompileStatusColorValid(t *testing.T) {
	got, ok := compileStatusColor(`\w+`)
	require.True(t, ok)
	want := color.Hex(styletokens.SuccessDefault.AsHex())
	assert.Equal(t, want, got)
}

func TestCompileStatusColorInvalid(t *testing.T) {
	got, ok := compileStatusColor(`(unclosed`)
	require.True(t, ok)
	want := color.Hex(styletokens.ErrorDefault.AsHex())
	assert.Equal(t, want, got)
}

// TestRenderHeadless renders one frame under the discard channel from a
// zero State, closed and open, and pins the W17 nil-Ids path (ADR-0267 W19).
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	var st State
	res := Render(Input{Ids: ids, ScopeKey: "t", Pattern: `\w+`, State: &st})
	require.NoError(t, res.Err)
	assert.False(t, res.Toggled)
	st.Pinned = true
	res = Render(Input{Ids: ids, ScopeKey: "t", Pattern: `\w+`, State: &st})
	require.NoError(t, res.Err)
	assert.NotNil(t, st.embedded, "an open inspector allocates its explorer")
	assert.Equal(t, `\w+`, st.lastSeededPat)
	assert.ErrorIs(t, Render(Input{Pattern: "x"}).Err, ErrNeedsIdsAndState)
	assert.Equal(t, 0, ids.Depth(), "the id stack is left balanced")
}
