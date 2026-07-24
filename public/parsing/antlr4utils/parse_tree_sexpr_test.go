package antlr4utils

import (
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ctxWithOffsets builds a rule context reporting the given token offsets.
// Passing nilStart / nilStop leaves the corresponding token unset, which is
// what ANTLR produces for a rule that failed to match.
func ctxWithOffsets(lo int, hi int, nilStart bool, nilStop bool) (out *antlr.BaseParserRuleContext) {
	// An empty source pair stands in for a token with no originating
	// stream, which is what NewCommonToken nil-checks for.
	var noSource antlr.TokenSourceCharStreamPair
	out = antlr.NewBaseParserRuleContext(nil, -1)
	if !nilStart {
		out.SetStart(antlr.NewCommonToken(&noSource, 1, antlr.TokenDefaultChannel, lo, lo))
	}
	if !nilStop {
		out.SetStop(antlr.NewCommonToken(&noSource, 1, antlr.TokenDefaultChannel, hi, hi))
	}
	return
}

func serializerFor(sql string) (out *ParseTreeSerializer) {
	return &ParseTreeSerializer{sql: sql, emitSourceInfo: true}
}

// Regression, 2026-07-24 review: formatSourceInterval2 sliced inst.sql on
// raw token offsets. A rule that failed to match carries a nil stop token,
// error recovery leaves synthetic tokens reporting -1, and a recovered stop
// can precede its start — each panicked. This serializer is the debug dump
// reached precisely when a parse went wrong, so those are its normal
// inputs, not exotic ones.
func TestFormatSourceInterval2SurvivesBrokenOffsets(t *testing.T) {
	const sql = "SELECT 1"
	cases := map[string]*antlr.BaseParserRuleContext{
		"nil stop token":    ctxWithOffsets(0, 0, false, true),
		"nil start token":   ctxWithOffsets(0, 0, true, false),
		"both tokens nil":   ctxWithOffsets(0, 0, true, true),
		"stop is -1":        ctxWithOffsets(0, -1, false, false),
		"start is -1":       ctxWithOffsets(-1, 3, false, false),
		"stop before start": ctxWithOffsets(5, 1, false, false),
		"stop past end":     ctxWithOffsets(0, 9999, false, false),
		"start past end":    ctxWithOffsets(9999, 9999, false, false),
		"both past end":     ctxWithOffsets(100, 200, false, false),
	}
	for name, rctxt := range cases {
		t.Run(name, func(t *testing.T) {
			var sb strings.Builder
			require.NotPanics(t, func() {
				require.NoError(t, serializerFor(sql).formatSourceInterval2(&sb, rctxt))
			})
			// The s-expression shape must stay intact so the surrounding
			// dump is still parseable.
			assert.True(t, strings.HasPrefix(sb.String(), " (("), "got %q", sb.String())
			assert.True(t, strings.HasSuffix(sb.String(), ")"), "got %q", sb.String())
		})
	}
}

func TestFormatSourceInterval2EmitsTheSpanWhenOffsetsAreSound(t *testing.T) {
	var sb strings.Builder
	require.NoError(t, serializerFor("SELECT 1").formatSourceInterval2(&sb, ctxWithOffsets(0, 5, false, false)))
	assert.Contains(t, sb.String(), `"SELECT"`, "the covered source text must still be emitted")
	assert.Contains(t, sb.String(), "((0 . 5)")
}

func TestFormatSourceInterval2SkippedWhenSourceInfoDisabled(t *testing.T) {
	var sb strings.Builder
	inst := &ParseTreeSerializer{sql: "SELECT 1", emitSourceInfo: false}
	require.NoError(t, inst.formatSourceInterval2(&sb, ctxWithOffsets(0, 5, false, false)))
	assert.Empty(t, sb.String())
}

func TestSliceSourceNeverPanics(t *testing.T) {
	const src = "abcdef"
	cases := []struct {
		lo, hi int
		want   string
	}{
		{0, 5, "abcdef"},
		{0, 0, "a"},
		{2, 3, "cd"},
		{0, -1, ""},   // empty span at the start
		{3, 2, ""},    // stop immediately before start: empty, not invalid
		{-1, 3, ""},   // synthetic start
		{-5, -5, ""},  // both synthetic
		{4, 99, "ef"}, // stop past the end is clamped
		{99, 99, ""},  // start past the end
		{6, 6, ""},    // start exactly at the end
		{5, 1, ""},    // stop well before start
	}
	for _, c := range cases {
		var got string
		require.NotPanicsf(t, func() { got = sliceSource(src, c.lo, c.hi) }, "lo=%d hi=%d", c.lo, c.hi)
		assert.Equalf(t, c.want, got, "sliceSource(%q, %d, %d)", src, c.lo, c.hi)
	}
}

// formatSourceInterval is reached from the same switch in serializeNode, so
// its token-stream indexing is no more trustworthy. Get() on a negative or
// past-the-end index panics, and the stream itself is optional.
func TestFormatSourceIntervalSurvivesBadIntervals(t *testing.T) {
	intervals := []antlr.Interval{
		{Start: -1, Stop: 3},
		{Start: 0, Stop: 9999},
		{Start: 5, Stop: 1},
		{Start: -10, Stop: -5},
		{Start: 0, Stop: 0},
	}
	for _, iv := range intervals {
		var sb strings.Builder
		// Nil token stream: NewParseTreeSerializer accepts one.
		inst := &ParseTreeSerializer{emitSourceInfo: true}
		require.NotPanicsf(t, func() {
			require.NoError(t, inst.formatSourceInterval(&sb, iv))
		}, "interval %+v with a nil stream", iv)
		assert.True(t, strings.HasSuffix(sb.String(), "|#"), "got %q", sb.String())
	}
}

func TestNewParseTreeSerializerRejectsUnknownValueTerminal(t *testing.T) {
	inst := &ParseTreeSerializer{symbolicNames: []string{"", "A", "B"}}
	require.NoError(t, inst.AddValueTerminal("B"))
	assert.Equal(t, []int{2}, inst.isValueTerminalSorted)
	require.Error(t, inst.AddValueTerminal("NOPE"))
}
