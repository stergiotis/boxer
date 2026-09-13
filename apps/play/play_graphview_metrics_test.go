package play

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The engine side of the Graphview panel (ADR-0231 §SD6, ADR-0232 §SD6/§SD8).

// gvModel builds the neutral model for an edge list, the way the panel does.
func gvModel(t *testing.T, src, tgt []string) netModel {
	t.Helper()
	er := netEdges(t, src, tgt, nil)
	t.Cleanup(er.Release)
	ec, reason := resolveNetworkEdges(er.Schema())
	require.Empty(t, reason)
	return buildNetModel(er, ec, nil, noVerticesClaim(),
		netCaps{vertices: graphviewMaxVertices, edges: graphviewMaxEdges})
}

// The claim the whole columnar change rests on: the engine's slots ARE the
// model's rows, so a metric column indexes the declaration with no join. The
// build checks it, so a regression is an error rather than a silently
// mis-sized graph.
func TestMetricGraphSlotsAreTheModelRows(t *testing.T) {
	for _, undirected := range []bool{false, true} {
		m := gvModel(t, []string{"delta", "alpha", "charlie"}, []string{"alpha", "bravo", "alpha"})
		gm := newGraphviewMetrics()
		require.NoError(t, gm.buildGraph(&m, undirected))
		require.Equal(t, m.NumVertices(), gm.g.NumVertices())
		for i, id := range gm.g.IDs() {
			assert.Equal(t, m.Key[i], id, "slot %d is not model row %d", i, i)
		}
		// And the fingerprint is a real key, not zero.
		assert.NotZero(t, gm.fp)
	}
}

// A degree column lands on the model's rows, so the vertex the query declared
// as a hub is the row carrying the largest degree.
func TestDegreeColumnIndexesTheModelRows(t *testing.T) {
	// alpha is reached by three others; the rest have degree 1.
	m := gvModel(t, []string{"b", "c", "d"}, []string{"a", "a", "a"})
	gm := newGraphviewMetrics()
	require.NoError(t, gm.buildGraph(&m, false))
	col, ok := gm.column(algo.MetricDegree)
	require.True(t, ok)
	require.Len(t, col.Values, m.NumVertices())

	byID := map[string]float64{}
	for i := range m.ID {
		byID[m.ID[i]] = col.Values[i]
	}
	assert.Equal(t, float64(3), byID["a"], "the hub's degree reached the hub's row")
	for _, leaf := range []string{"b", "c", "d"} {
		assert.Equal(t, float64(1), byID[leaf])
	}
}

// A column is computed once per key and cached: a running simulation must not
// recompute a metric per frame (ADR-0231 §SD6).
func TestMetricColumnIsCachedOnTheTopology(t *testing.T) {
	m := gvModel(t, []string{"a", "b"}, []string{"b", "c"})
	gm := newGraphviewMetrics()
	require.NoError(t, gm.buildGraph(&m, false))

	first, ok := gm.column(algo.MetricPageRank)
	require.True(t, ok)
	require.Len(t, gm.cols, 1)
	second, ok := gm.column(algo.MetricPageRank)
	require.True(t, ok)
	require.Len(t, gm.cols, 1, "a second read recomputed the metric")
	// The same slice, not an equal one: the cache hands back what it holds.
	require.Equal(t, &first.Values[0], &second.Values[0])

	// A rebuild drops it, because the topology it was computed for is gone.
	require.NoError(t, gm.buildGraph(&m, false))
	assert.Empty(t, gm.cols)
}

// A seeded metric needs a seed set, and moving the set drops only the columns
// that depend on it.
func TestSeededMetricsFollowTheSeedSet(t *testing.T) {
	m := gvModel(t, []string{"a", "b"}, []string{"b", "c"})
	gm := newGraphviewMetrics()
	require.NoError(t, gm.buildGraph(&m, false))

	_, ok := gm.column(algo.MetricDistance)
	assert.False(t, ok, "with nothing selected there is nothing to measure from")

	gm.setSeeds([]int32{0})
	_, ok = gm.column(algo.MetricDistance)
	require.True(t, ok)
	_, ok = gm.column(algo.MetricDegree)
	require.True(t, ok)
	require.Len(t, gm.cols, 2)

	gm.setSeeds([]int32{1})
	require.Len(t, gm.cols, 1, "the seeded column went, the unseeded one stayed")
	for k := range gm.cols {
		assert.False(t, k.metric.IsSeeded())
	}
	// Re-declaring the same set is not a change.
	gm.setSeeds([]int32{1})
	require.Len(t, gm.cols, 1)
}

// Under an undirected reading a direction-bearing metric collapses onto its
// sibling rather than being refused — the engine owns that mapping.
func TestUndirectedReadingMapsThroughSymmetric(t *testing.T) {
	m := gvModel(t, []string{"a", "b"}, []string{"b", "c"})
	gm := newGraphviewMetrics()
	require.NoError(t, gm.buildGraph(&m, true))
	in, ok := gm.column(algo.MetricInDegree)
	require.True(t, ok)
	deg, ok := gm.column(algo.MetricDegree)
	require.True(t, ok)
	assert.Equal(t, deg.Values, in.Values, "in_degree reads as degree once symmetrised")
	require.Len(t, gm.cols, 1, "both names resolved to one cached column")
}

func TestParseGraphviewSelector(t *testing.T) {
	hasWeight := func(s string) bool { return s == networkWeightCol }

	sel, reason := parseGraphviewSelector("pagerank", graphviewChannelSize, hasWeight)
	require.Empty(t, reason)
	assert.Equal(t, algo.MetricPageRank, sel.Metric)
	assert.False(t, sel.Invert)

	// A leading '-' inverts the ramp, the spelling ORDER BY already has.
	sel, reason = parseGraphviewSelector("-distance", graphviewChannelOpacity, hasWeight)
	require.Empty(t, reason)
	assert.Equal(t, algo.MetricDistance, sel.Metric)
	assert.True(t, sel.Invert)

	// A column of the contract is spent on the channel instead of a metric.
	sel, reason = parseGraphviewSelector("weight", graphviewChannelSize, hasWeight)
	require.Empty(t, reason)
	assert.Equal(t, algo.MetricNone, sel.Metric)
	assert.Equal(t, "weight", sel.Column)

	// The empty selector leaves the channel alone.
	sel, reason = parseGraphviewSelector("  ", graphviewChannelSize, hasWeight)
	require.Empty(t, reason)
	assert.True(t, sel.IsZero())

	// The refusals are the parser's and the metric's Kind, not a table of
	// this panel's own.
	for _, ch := range []graphviewChannelE{graphviewChannelSize, graphviewChannelOpacity} {
		_, reason = parseGraphviewSelector("component", ch, hasWeight)
		assert.Contains(t, reason, "label rather than a quantity", "%s took a categorical metric", ch)
	}
	for _, ch := range []graphviewChannelE{graphviewChannelTone, graphviewChannelAura} {
		sel, reason = parseGraphviewSelector("component", ch, hasWeight)
		require.Empty(t, reason, "%s should group by a label", ch)
		assert.Equal(t, algo.MetricComponent, sel.Metric)
	}
	_, reason = parseGraphviewSelector("nonesuch", graphviewChannelSize, hasWeight)
	assert.Contains(t, reason, "no column or metric of that name")
}

// The size channel ramps over the metric a selector names, and an absent value
// takes the style default rather than the bottom of the ramp.
func TestSizeChannelSpendsTheSelectedMetric(t *testing.T) {
	m := gvModel(t, []string{"b", "c", "d"}, []string{"a", "a", "a"})
	d := NewGraphviewDriver(nil, nil)
	d.sizeBy, d.sizeBySet = "degree", true
	d.rebuild(&m)
	require.Empty(t, d.sizeReason)

	hub, ok := gvNodeByName(d, "a")
	require.True(t, ok)
	leaf, ok := gvNodeByName(d, "b")
	require.True(t, ok)
	assert.InDelta(t, graphviewMaxRadius, hub.Radius, 0.01, "the hub takes the ceiling")
	assert.Greater(t, hub.Radius, leaf.Radius)

	// A seeded metric with nothing selected says so and leaves the channel
	// on the contract's column rather than blanking every node.
	d2 := NewGraphviewDriver(nil, nil)
	d2.sizeBy, d2.sizeBySet = "distance", true
	m2 := gvModel(t, []string{"b"}, []string{"a"})
	d2.rebuild(&m2)
	assert.Contains(t, d2.sizeReason, "select a node")

	// An unparseable selector is refused by name.
	d3 := NewGraphviewDriver(nil, nil)
	d3.sizeBy, d3.sizeBySet = "nonesuch", true
	m3 := gvModel(t, []string{"b"}, []string{"a"})
	d3.rebuild(&m3)
	assert.Contains(t, d3.sizeReason, "no column or metric")
}

// An unreached vertex under `distance` is absent, not zero, so it must take
// the style default rather than ramping to the smallest radius.
func TestAbsentMetricValueTakesTheStyleDefault(t *testing.T) {
	// Two components, so "c" is unreachable from "a".
	m := gvModel(t, []string{"a", "c"}, []string{"b", "d"})
	gm := newGraphviewMetrics()
	require.NoError(t, gm.buildGraph(&m, false))
	gm.setSeeds([]int32{int32(netRowOf(t, &m, "a"))})
	col, ok := gm.column(algo.MetricDistance)
	require.True(t, ok)
	assert.True(t, math.IsNaN(col.Values[netRowOf(t, &m, "c")]), "unreached is absent")
	// The radius it maps to is ABSENT — NaN, the columnar declaration's "not
	// declared for this row" (ADR-0232 §SD4) — which takes the style
	// default. A zero would be an explicit zero-radius node, and the bottom
	// of the ramp would be worse still: an unreachable vertex would read as
	// the nearest one.
	assert.True(t, math.IsNaN(float64(graphviewRadius(col.Values[netRowOf(t, &m, "c")], 4))),
		"an absent value must not ramp")
	assert.False(t, math.IsNaN(float64(graphviewRadius(col.Values[netRowOf(t, &m, "b")], 4))),
		"a reached vertex still ramps")
}

// The chrome's vocabulary is the engine's, minus what cannot size a node.
func TestSizeOptionsComeFromTheEngine(t *testing.T) {
	opts := graphviewSizeOptions()
	require.Equal(t, networkWeightCol, opts[0], "the contract's own column comes first")
	assert.Contains(t, opts, "pagerank")
	assert.NotContains(t, opts, "component", "a label cannot size a node")
	assert.NotContains(t, opts, "scc")
	for _, m := range algo.Metrics() {
		if m.Kind() == algo.KindCategorical {
			continue
		}
		assert.Contains(t, opts, m.String(), "the chrome dropped a metric the engine computes")
	}
}
