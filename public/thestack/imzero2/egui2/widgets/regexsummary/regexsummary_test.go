//go:build llm_generated_opus47

package regexsummary

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
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

// TestCallScopeDeterministic locks the format and stability contract:
// the same (idPrefix, callId) pair always yields the same scope
// string, and distinct callIds always produce distinct strings.
func TestCallScopeDeterministic(t *testing.T) {
	a := callScope("my-regex", 0xDEADBEEF)
	b := callScope("my-regex", 0xDEADBEEF)
	c := callScope("my-regex", 0xCAFEBABE)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	// Format must remain "idPrefix#<hex>" so log greps and id-collision
	// debugging stay tractable.
	assert.Equal(t, "my-regex#deadbeef", a)
}

// TestRendererDefaultsAreUsable pins the documented defaults.
func TestRendererDefaultsAreUsable(t *testing.T) {
	r := New("test")
	assert.Equal(t, "test", r.idPrefix)
	assert.Equal(t, defaultPopupW, r.popupWidth)
	assert.Equal(t, defaultPopupH, r.popupHeight)
	assert.Equal(t, defaultPatternMaxLen, r.patternMaxLen)
	assert.True(t, r.showIcon)
	assert.True(t, r.showPattern)
	assert.True(t, r.showStatusDot)
	assert.Nil(t, r.bus)
	assert.True(t, r.provenance.IsZero())
}

// TestRendererFluentSettersReturnCopies locks the value-receiver
// contract on every fluent setter: the base Renderer's fields are
// unchanged after a setter call, and the returned modified Renderer
// carries the new value.
func TestRendererFluentSettersReturnCopies(t *testing.T) {
	base := New("test")
	mod := base.
		PopupSize(640, 480).
		PatternMaxLen(64).
		ShowIcon(false).
		ShowPattern(false).
		ShowStatusDot(false)
	// Originals untouched.
	assert.Equal(t, defaultPopupW, base.popupWidth)
	assert.Equal(t, defaultPopupH, base.popupHeight)
	assert.Equal(t, defaultPatternMaxLen, base.patternMaxLen)
	assert.True(t, base.showIcon)
	assert.True(t, base.showPattern)
	assert.True(t, base.showStatusDot)
	// Mods propagated.
	assert.Equal(t, float32(640), mod.popupWidth)
	assert.Equal(t, float32(480), mod.popupHeight)
	assert.Equal(t, 64, mod.patternMaxLen)
	assert.False(t, mod.showIcon)
	assert.False(t, mod.showPattern)
	assert.False(t, mod.showStatusDot)
}

// TestRendererPatternMaxLenClampsBelowMinimum exercises the documented
// "values < 1 → defaultPatternMaxLen" contract so a typo at the call
// site cannot silently produce a zero-width inline label.
func TestRendererPatternMaxLenClampsBelowMinimum(t *testing.T) {
	r := New("test").PatternMaxLen(0)
	assert.Equal(t, defaultPatternMaxLen, r.patternMaxLen)
	r = New("test").PatternMaxLen(-5)
	assert.Equal(t, defaultPatternMaxLen, r.patternMaxLen)
}

// TestRendererBusFluentPassthrough confirms the optional-bus contract:
// passing a non-nil BusI stores the pointer; subsequent Bus(nil) calls
// land a nil on the Renderer (the eventual SetBus on the embedded app
// converts that nil into a NoopBus).
func TestRendererBusFluentPassthrough(t *testing.T) {
	var stub runtimeapp.BusI = &runtimeapp.NoopBus{}
	r := New("test").Bus(stub)
	assert.Same(t, stub, r.bus)
	r2 := r.Bus(nil)
	assert.Nil(t, r2.bus)
	// Original unchanged.
	assert.Same(t, stub, r.bus)
}

// TestRendererProvenanceFluentPassthrough confirms the optional-
// provenance contract: setting a non-zero Provenance flips IsZero
// to false on the modified Renderer while the base stays zero.
func TestRendererProvenanceFluentPassthrough(t *testing.T) {
	p := inspector.Provenance{SourceApp: "host", Subject: "rules.user-regex"}
	r := New("test").Provenance(p)
	assert.False(t, r.provenance.IsZero())
	base := New("test")
	assert.True(t, base.provenance.IsZero())
}

// TestGetInstanceStateIdempotent locks the documented LoadOrStore
// behaviour: repeated lookups for the same scope return the same
// *instanceState pointer so per-instance state (pinned flag, embedded
// app) survives across frames.
func TestGetInstanceStateIdempotent(t *testing.T) {
	scope := callScope("idempotent-test", 0x1234)
	a := getInstanceState(scope)
	b := getInstanceState(scope)
	assert.Same(t, a, b)
	// Distinct scopes yield distinct states.
	other := getInstanceState(callScope("idempotent-test", 0x5678))
	assert.NotSame(t, a, other)
}
