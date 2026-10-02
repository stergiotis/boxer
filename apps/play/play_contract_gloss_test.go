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

// The overlay lanes used to fail without a word: the lane's own error never
// left the demand.
func TestSeriesAuxNote(t *testing.T) {
	notes := seriesAuxNote(seriesScoresNodeID, errors.New("UNKNOWN_TABLE\nmore"), false, nil)
	assert.Equal(t, []string{"`scores` query failed: UNKNOWN_TABLE"}, notes, "an error outranks the rest, first line only")

	notes = seriesAuxNote(seriesSpansNodeID, nil, true, nil)
	assert.Equal(t, []string{"`spans` …"}, notes)

	served := glossSchema(arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64})
	assert.Empty(t, seriesAuxNote(seriesScoresNodeID, nil, false, served),
		"a served result the channel refuses is the dispatcher's to report")
	assert.Empty(t, seriesAuxNote(seriesScoresNodeID, nil, false, nil),
		"no split node, nothing served: no note")
}

// An optional channel offered a real result and refusing it is reported; one
// offered nothing (no such CTE) is not, since that is the ordinary case.
func TestNegotiateReportsRefusedOptionalChannel(t *testing.T) {
	flows := glossSchema(
		arrow.Field{Name: "source", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "target", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "value", Type: arrow.PrimitiveTypes.Float64},
	)
	noID := glossSchema(arrow.Field{Name: "label", Type: arrow.BinaryTypes.String})

	filled, refused, reject := negotiateChannels(sankeyPanel{}, map[ChannelID]channelInput{
		chFlows: {node: sankeyFlowsNodeID, schema: flows},
		chNodes: {node: sankeyNodesNodeID, schema: noID},
	})
	require.Empty(t, reject)
	assert.Contains(t, filled, chFlows)
	assert.NotContains(t, filled, chNodes)
	require.Len(t, refused, 1)
	assert.Equal(t, "nodes", refused[0].Label)
	assert.Contains(t, refused[0].String(), "`nodes` not used: ")

	_, refused, reject = negotiateChannels(sankeyPanel{}, map[ChannelID]channelInput{
		chFlows: {node: sankeyFlowsNodeID, schema: flows},
	})
	require.Empty(t, reject)
	assert.Empty(t, refused, "no `nodes` CTE offered: nothing to report")

	_, refused, reject = negotiateChannels(sankeyPanel{}, map[ChannelID]channelInput{
		chNodes: {node: sankeyNodesNodeID, schema: noID},
	})
	assert.NotEmpty(t, reject, "the required channel's reason wins")
	assert.Empty(t, refused)
}

func TestKanbanContractMatchesGlossLabel(t *testing.T) {
	k, reason := resolveKanbanColumns(glossSchema(
		arrow.Field{Name: "lane", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "title@text/markdown", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "dot_open@warning", Type: arrow.PrimitiveTypes.Uint64},
	))
	require.Empty(t, reason)
	assert.Equal(t, 1, k.titleCol)
	require.Len(t, k.dots, 1, "a dot's `@` is a tone, not a gloss: still a tally")
	assert.Equal(t, "open", k.dots[0].label)
}

func TestNetworkContractMatchesGlossLabel(t *testing.T) {
	ec, reason := resolveNetworkEdges(glossSchema(
		arrow.Field{Name: "source", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "target", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "label@text/markdown", Type: arrow.BinaryTypes.String},
	))
	require.Empty(t, reason)
	assert.Equal(t, 2, ec.labelCol)

	vc, reason := resolveNetworkVertices(glossSchema(
		arrow.Field{Name: "id", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "weight@gloss/bytes", Type: arrow.PrimitiveTypes.Float64},
	))
	require.Empty(t, reason)
	assert.Equal(t, 1, vc.weightCol)
}

func TestSankeyContractMatchesGlossLabel(t *testing.T) {
	fc, reason := resolveSankeyFlows(glossSchema(
		arrow.Field{Name: "source", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "target", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "value@gloss/bytes", Type: arrow.PrimitiveTypes.Float64},
	))
	require.Empty(t, reason)
	assert.Equal(t, 2, fc.valCol)
}

// A Graphview selector names a vertices column by its label; the exact name
// wins when a result carries both spellings.
func TestFieldIndexByLabel(t *testing.T) {
	s := glossSchema(
		arrow.Field{Name: "mass@gloss/bytes", Type: arrow.PrimitiveTypes.Float64},
		arrow.Field{Name: "id", Type: arrow.BinaryTypes.String},
	)
	assert.Equal(t, 0, fieldIndexByLabel(s, "mass"))
	assert.Equal(t, 1, fieldIndexByLabel(s, "id"))
	assert.Equal(t, -1, fieldIndexByLabel(s, "nope"))

	both := glossSchema(
		arrow.Field{Name: "mass@gloss/bytes", Type: arrow.PrimitiveTypes.Float64},
		arrow.Field{Name: "mass", Type: arrow.PrimitiveTypes.Float64},
	)
	assert.Equal(t, 1, fieldIndexByLabel(both, "mass"))
}
