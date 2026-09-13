package algo

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The per-vertex metric vocabulary (ADR-0232 §SD6). A consumer that offers a
// user a choice of metric — an encoding selector in a panel, a fact kind, a
// navigation layer's relevance — names a [MetricE] rather than keeping a
// table of strings of its own, so the list exists once and a metric added to
// this package reaches every consumer by being added here.

// nan64 is the "no value for this slot" entry of a [MetricColumn].
var nan64 = math.NaN()

// MetricE names a per-vertex metric this package can compute. The zero value
// is [MetricNone], which names none, so an unparsed selector and an unset
// field are the same thing.
type MetricE uint8

const (
	MetricNone MetricE = iota
	MetricDegree
	MetricInDegree
	MetricOutDegree
	MetricPageRank
	MetricBetweenness
	MetricKCore
	MetricTriangles
	MetricClustering
	MetricClique
	MetricComponent
	MetricSCC
	MetricComponentSize
	MetricDistance
	MetricDistanceIn
	MetricDistanceOut
	MetricRelevance
)

// KindE says what a metric's values are, which is what decides the channels
// they can drive: an ordinal metric is a quantity and can size or ramp, a
// categorical one is a label and can only group or colour by distinct value.
type KindE uint8

const (
	// KindNone is the kind of [MetricNone].
	KindNone KindE = iota
	KindOrdinal
	KindCategorical
)

// metricNames is the spelling of every metric, indexed by the value. It is
// the single table [MetricE.String] and [ParseMetric] both read, so the two
// cannot disagree.
var metricNames = [...]string{
	MetricNone:          "",
	MetricDegree:        "degree",
	MetricInDegree:      "in_degree",
	MetricOutDegree:     "out_degree",
	MetricPageRank:      "pagerank",
	MetricBetweenness:   "betweenness",
	MetricKCore:         "kcore",
	MetricTriangles:     "triangles",
	MetricClustering:    "clustering",
	MetricClique:        "clique",
	MetricComponent:     "component",
	MetricSCC:           "scc",
	MetricComponentSize: "component_size",
	MetricDistance:      "distance",
	MetricDistanceIn:    "distance_in",
	MetricDistanceOut:   "distance_out",
	MetricRelevance:     "relevance",
}

// String is the metric's spelling, and the empty string for [MetricNone].
func (inst MetricE) String() string {
	if int(inst) >= len(metricNames) {
		return ""
	}
	return metricNames[inst]
}

// IsValid reports whether the value names a metric this package computes.
func (inst MetricE) IsValid() bool {
	return inst != MetricNone && int(inst) < len(metricNames)
}

// ParseMetric resolves a spelling. ok is false for anything this package does
// not compute, which is the refusal a consumer reports rather than guessing.
func ParseMetric(s string) (m MetricE, ok bool) {
	for i, name := range metricNames {
		if name != "" && name == s {
			return MetricE(i), true
		}
	}
	return MetricNone, false
}

// Metrics lists the vocabulary in declaration order — every valid value,
// which is what a consumer offers as a choice and what a completeness test
// iterates.
func Metrics() (out []MetricE) {
	out = make([]MetricE, 0, len(metricNames)-1)
	for i := 1; i < len(metricNames); i++ {
		out = append(out, MetricE(i))
	}
	return
}

// Kind reports whether the metric's values are quantities or labels.
func (inst MetricE) Kind() KindE {
	switch inst {
	case MetricComponent, MetricSCC:
		return KindCategorical
	case MetricNone:
		return KindNone
	}
	if !inst.IsValid() {
		return KindNone
	}
	return KindOrdinal
}

// Symmetric is the metric this one reads as once the graph is symmetrised.
// A direction-bearing metric collapses onto its undirected sibling — there is
// one degree on an undirected graph, and its strongly connected components
// are its connected components — so a consumer that offers a directed and an
// undirected reading maps the selector through this rather than refusing it.
// Every other metric is its own symmetric form.
func (inst MetricE) Symmetric() MetricE {
	switch inst {
	case MetricInDegree, MetricOutDegree:
		return MetricDegree
	case MetricSCC:
		return MetricComponent
	case MetricDistanceIn, MetricDistanceOut:
		return MetricDistance
	}
	return inst
}

// IsSeeded reports whether the metric is computed relative to a source set,
// and so needs [ComputeOptions.Sources] and is recomputed when that set moves.
func (inst MetricE) IsSeeded() bool {
	switch inst {
	case MetricDistance, MetricDistanceIn, MetricDistanceOut, MetricRelevance:
		return true
	}
	return false
}

// MetricColumn is one metric over a graph's slots (ADR-0229 §SD5): values
// aligned with the [csr.Graph]'s slot index, so a consumer whose own rows are
// in slot order indexes them directly and writes no join.
type MetricColumn struct {
	Metric MetricE
	Kind   KindE
	// Values is slot-aligned. NaN is "no value for this slot" rather than
	// zero: an unreached vertex in the distance family, a vertex with no
	// neighbour pair under `clustering`. A ramp must not place those at its
	// bottom, which is what a zero would do.
	Values []float64
	Truncation
}

// ComputeOptions carries the source set the seeded metrics need and the
// budgets of the algorithms behind the others. Each embedded struct is the
// one the underlying function takes, so tuning a budget here is tuning the
// same field as calling that function directly.
type ComputeOptions struct {
	// Sources seeds the metrics [MetricE.IsSeeded] reports — the distance
	// family and `relevance`. Slots, not ids.
	Sources     []int32
	PageRank    PageRankOptions
	Betweenness BetweennessOptions
	Cliques     CliqueOptions
	BFS         BFSOptions
}

// Compute runs one metric and returns it as a column. It dispatches to the
// functions in this package and is identical to calling them: the options it
// is handed travel unchanged, so a caller that wants a whole result — a BFS's
// parents, a clique listing's members — calls the function and loses nothing
// by it.
//
// A metric that needs a source set and is given none is an error rather than
// an empty column, because the difference between "nothing is near the
// selection" and "nothing was selected" is one a consumer must be able to
// report.
func Compute(ctx context.Context, e *engine.Engine, g *csr.Graph, m MetricE, opts ComputeOptions) (c MetricColumn, err error) {
	if !m.IsValid() {
		err = eb.Build().Uint8("metric", uint8(m)).Errorf("unknown metric")
		return
	}
	if m.IsSeeded() && len(opts.Sources) == 0 {
		err = eb.Build().Str("metric", m.String()).Errorf("metric needs a source set")
		return
	}
	c.Metric, c.Kind = m, m.Kind()
	n := g.NumVertices()
	switch m {
	case MetricDegree:
		out, in := Degrees(g)
		c.Values = make([]float64, n)
		for v := range n {
			// On an undirected container every edge sits in both rows, so the
			// out-degree already is the degree; on a directed one the total
			// degree is the sum.
			if g.IsDirected() {
				c.Values[v] = float64(out[v]) + float64(in[v])
			} else {
				c.Values[v] = float64(out[v])
			}
		}
	case MetricInDegree:
		_, in := Degrees(g)
		c.Values = int32Column(in)
	case MetricOutDegree:
		out, _ := Degrees(g)
		c.Values = int32Column(out)
	case MetricPageRank:
		r := PageRank(ctx, e, g, opts.PageRank)
		c.Values, c.Truncation = r.Rank, r.Truncation
	case MetricRelevance:
		// The seeded rank: the same sweep with the selection as its teleport
		// set (ADR-0232 §SD7), which is the smooth form of `distance`.
		po := opts.PageRank
		po.Teleport = opts.Sources
		r := PageRank(ctx, e, g, po)
		c.Values, c.Truncation = r.Rank, r.Truncation
	case MetricBetweenness:
		var r BetweennessResult
		if r, err = Betweenness(ctx, e, g, opts.Betweenness); err != nil {
			return
		}
		c.Values, c.Truncation = r.Score, r.Truncation
	case MetricKCore:
		r := KCore(ctx, g)
		c.Values, c.Truncation = int32Column(r.Core), r.Truncation
	case MetricTriangles:
		to := TriangleOptions{PerVertex: true}
		r := Triangles(ctx, e, g, to)
		c.Values, c.Truncation = uint32Column(r.PerVertex), r.Truncation
	case MetricClustering:
		r := Triangles(ctx, e, g, TriangleOptions{PerVertex: true})
		c.Values, c.Truncation = LocalClustering(g, r.PerVertex), r.Truncation
	case MetricClique:
		r := MaximalCliques(ctx, g, opts.Cliques)
		c.Values, c.Truncation = int32Column(CliqueSizes(g, r)), r.Truncation
	case MetricComponent:
		r := ConnectedComponents(ctx, g)
		c.Values, c.Truncation = int32Column(r.Comp), r.Truncation
	case MetricSCC:
		r := StronglyConnectedComponents(ctx, g)
		c.Values, c.Truncation = int32Column(r.Comp), r.Truncation
	case MetricComponentSize:
		r := ConnectedComponents(ctx, g)
		c.Values, c.Truncation = int32Column(ComponentSizes(r.Comp)), r.Truncation
	case MetricDistance, MetricDistanceIn, MetricDistanceOut:
		bo := opts.BFS
		bo.Direction = bfsDirection(m)
		var r BFSResult
		if r, err = BFS(ctx, e, g, opts.Sources, bo); err != nil {
			return
		}
		c.Values = make([]float64, len(r.Depth))
		for v, d := range r.Depth {
			if d < 0 {
				c.Values[v] = nan64 // unreached: absent, not "zero hops"
				continue
			}
			c.Values[v] = float64(d)
		}
		c.Truncation = r.Truncation
	}
	return
}

// bfsDirection is the adjacency each distance metric walks. `distance` reads
// the graph as undirected, which is what makes its symmetric form itself.
func bfsDirection(m MetricE) engine.DirectionE {
	switch m {
	case MetricDistanceIn:
		return engine.DirectionIn
	case MetricDistanceOut:
		return engine.DirectionOut
	}
	return engine.DirectionBoth
}

func int32Column(src []int32) (out []float64) {
	out = make([]float64, len(src))
	for i, v := range src {
		out[i] = float64(v)
	}
	return
}

func uint32Column(src []uint32) (out []float64) {
	out = make([]float64, len(src))
	for i, v := range src {
		out[i] = float64(v)
	}
	return
}
