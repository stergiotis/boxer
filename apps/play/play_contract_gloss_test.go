package play

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A named-column contract is a question about what a column IS, not how it
// renders, so a column that declares a gloss in its name (ADR-0186 §SD7) must
// still claim its slot. These cases pin that for the panes that matched on the
// raw name until 2026-10-02: Distribution, Icicle / Treemap, Chart, and the
// Series score overlay.

func glossSchema(fields ...arrow.Field) *arrow.Schema { return arrow.NewSchema(fields, nil) }

func TestDistContractMatchesGlossLabel(t *testing.T) {
	floats := arrow.ListOf(arrow.PrimitiveTypes.Float64)
	k, reason := resolveDistColumns(glossSchema(
		arrow.Field{Name: "series", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Uint64},
		arrow.Field{Name: "ps", Type: floats},
		arrow.Field{Name: "qs@gloss/bytes", Type: floats},
	))
	require.Empty(t, reason)
	assert.Equal(t, 3, k.qsCol)
}

func TestHierarchyContractMatchesGlossLabel(t *testing.T) {
	cl, reason := resolveHierarchy(glossSchema(
		arrow.Field{Name: "stack", Type: arrow.ListOf(arrow.BinaryTypes.String)},
		arrow.Field{Name: "value@gloss/bytes", Type: arrow.PrimitiveTypes.Uint64},
	), icicleForm)
	require.Empty(t, reason)
	assert.Equal(t, hierModeFolded, cl.mode)
	assert.Equal(t, 1, cl.valueCol)
}

func TestChartContractMatchesGlossLabel(t *testing.T) {
	k, reason := resolveChartColumns(glossSchema(
		arrow.Field{Name: "x", Type: arrow.PrimitiveTypes.Int64},
		arrow.Field{Name: "y", Type: arrow.PrimitiveTypes.Int64},
		arrow.Field{Name: "z@gloss/bytes", Type: arrow.PrimitiveTypes.Float64},
	))
	require.Empty(t, reason)
	assert.Equal(t, chartReadingGrid, k.reading)
	assert.Equal(t, 2, k.zCol)
}

func TestSeriesScoresMatchesGlossLabel(t *testing.T) {
	_, reason := acceptSeriesScores(glossSchema(
		arrow.Field{Name: "t", Type: &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}},
		arrow.Field{Name: "score@gloss/temperature;unit=C", Type: arrow.PrimitiveTypes.Float64},
	))
	assert.Empty(t, reason)
}

// The overlay lanes used to fail without a word: the dispatcher drops an
// optional channel's reject, and the lane's own error never left the demand.
func TestSeriesAuxNote(t *testing.T) {
	bad := glossSchema(arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64})

	notes := seriesAuxNote(seriesScoresNodeID, errors.New("UNKNOWN_TABLE\nmore"), false, nil, acceptSeriesScores)
	assert.Equal(t, []string{"`scores` query failed: UNKNOWN_TABLE"}, notes, "an error outranks the rest, first line only")

	notes = seriesAuxNote(seriesSpansNodeID, nil, true, nil, acceptSeriesSpans)
	assert.Equal(t, []string{"`spans` …"}, notes)

	notes = seriesAuxNote(seriesScoresNodeID, nil, false, bad, acceptSeriesScores)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "`scores` not drawn: A `scores` CTE needs")

	assert.Empty(t, seriesAuxNote(seriesScoresNodeID, nil, false, nil, acceptSeriesScores),
		"no split node, nothing served: no note")
}
