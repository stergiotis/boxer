package sqleditor

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Bind publishes the caret site beside the entity (ADR-0190 §SD2), and it is
// scoped to the caret's own statement: a bracket a neighbour left open is not
// one this caret is inside.
func TestBindPublishesTheSite(t *testing.T) {
	buf := "SELECT LW_COMPONENT('Sys"
	e := New()
	n := uint64(len([]rune(buf)))
	e.caretPacked = n | n<<32
	res := e.Bind(Frame{IDSlot: "t", Value: &buf})

	f, ok := res.Site.InnerFrame()
	require.True(t, ok)
	assert.Equal(t, "LW_COMPONENT", f.Callee)
	assert.Equal(t, 0, f.Ordinal)
	assert.Equal(t, "Sys", res.Site.PartialText)
	require.NotNil(t, res.Site.Literal)
	assert.False(t, res.Site.Literal.Terminated())
	assert.Equal(t, res.Entity, res.Site.Entity)
}

func TestSiteIsScopedToTheCaretsStatement(t *testing.T) {
	buf := "SELECT f(1;\nSELECT 2"
	e := New()
	n := uint64(len([]rune(buf)))
	e.caretPacked = n | n<<32
	res := e.Bind(Frame{IDSlot: "t", Value: &buf})

	assert.Empty(t, res.Site.Frames, "the previous statement's open paren is not this caret's frame")
	assert.Empty(t, res.Site.Open)
}

func TestSpansWithinKeepsCanonicalOffsets(t *testing.T) {
	buf := "SELECT 1;\nSELECT 2"
	spans := highlight.HighlightLex(buf)
	ranges, _ := BodyStatementRanges(buf)
	require.Len(t, ranges, 2)
	out := spansWithin(spans, ranges[1], true)
	require.NotEmpty(t, out)
	assert.GreaterOrEqual(t, out[0].Start, ranges[1].Src.Start)
	assert.LessOrEqual(t, out[len(out)-1].Stop, len(buf))
}
