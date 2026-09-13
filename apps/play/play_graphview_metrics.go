package play

import (
	"context"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_graphview_metrics.go is the analytics engine's side of the Graphview
// panel (ADR-0229 §SD6, ADR-0231 §SD6): the CSR built from the model's edge
// list, and the metric columns an encoding selector spends.
//
// The engine's slots are ascending id and the model's rows are sorted by
// interned id (ADR-0232 §SD9), so **a metric column indexes the model's rows
// and the widget's arrays directly** — there is no join here, and
// buildGraph asserts the alignment rather than trusting it.
//
// `csr.Graph.Fingerprint` is the one topology key (ADR-0232 §SD8): the CSR is
// rebuilt when the model is, and every metric is cached against that
// fingerprint plus what else it depends on, so a running simulation computes
// nothing per frame.

// graphviewMetricKey is what a cached column was computed for. The seed is
// folded in because the seeded metrics move with the selection, and the
// direction because symmetrising changes every one of them.
type graphviewMetricKey struct {
	metric     algo.MetricE
	undirected bool
	seed       uint64
}

// graphviewMetrics owns the CSR and the computed columns for one model.
type graphviewMetrics struct {
	eng *engine.Engine

	g          *csr.Graph
	fp         uint64 // the CSR's content fingerprint: SD8's one topology key
	undirected bool
	// rows is the model row count the CSR was built for; a column is only
	// handed out when it matches, so a stale column cannot index a new model.
	rows int

	cols map[graphviewMetricKey]algo.MetricColumn
	// seeds is the seed set of the last computation, as model rows.
	seeds     []int32
	seedKey   uint64
	truncated map[algo.MetricE]algo.Truncation
}

func newGraphviewMetrics() *graphviewMetrics {
	return &graphviewMetrics{
		eng:       engine.New(0),
		cols:      make(map[graphviewMetricKey]algo.MetricColumn, 4),
		truncated: make(map[algo.MetricE]algo.Truncation, 4),
	}
}

// buildGraph rebuilds the CSR for a model and drops every column computed for
// the old one. Called once per declaration rebuild, not per frame.
//
// The vertex list is passed explicitly so an isolated vertex — one the edge
// list never names — still gets a slot, without which every column after it
// would be off by one against the model's rows.
func (inst *graphviewMetrics) buildGraph(m *netModel, undirected bool) (err error) {
	inst.g, inst.fp, inst.rows = nil, 0, 0
	clear(inst.cols)
	clear(inst.truncated)
	if m.NumVertices() == 0 {
		return
	}
	g, err := csr.BuildE(m.From, m.To, nil, csr.Options{Directed: !undirected, Vertices: m.Key})
	if err != nil {
		return eb.Build().Errorf("graphview: building the metric graph: %w", err)
	}
	// The alignment the whole columnar change exists for: the CSR's slots are
	// ascending id and the model's rows are sorted by interned id, so slot i
	// IS row i. Checked rather than assumed, because everything downstream
	// indexes one with the other.
	ids := g.IDs()
	if len(ids) != m.NumVertices() {
		return eb.Build().Int("slots", len(ids)).Int("rows", m.NumVertices()).
			Errorf("graphview: the metric graph and the model disagree on the vertex count")
	}
	for i, id := range ids {
		if id != m.Key[i] {
			return eb.Build().Int("slot", i).Uint64("id", id).Uint64("key", m.Key[i]).
				Errorf("graphview: the metric graph's slots are not the model's rows")
		}
	}
	inst.g, inst.fp, inst.undirected, inst.rows = g, g.Fingerprint(), undirected, m.NumVertices()
	return
}

// setSeeds declares the seed set the seeded metrics are computed from, as
// model rows. A change drops the columns that depend on it and keeps the rest,
// which is why the key carries the seed rather than the whole cache being
// cleared.
func (inst *graphviewMetrics) setSeeds(rows []int32) {
	key := hashSlots(rows)
	if key == inst.seedKey {
		return
	}
	inst.seedKey = key
	inst.seeds = append(inst.seeds[:0], rows...)
	for k := range inst.cols {
		if k.metric.IsSeeded() {
			delete(inst.cols, k)
		}
	}
}

// hashSlots is an order-sensitive FNV-1a over a slot set, for the cache key.
func hashSlots(rows []int32) (h uint64) {
	h = 1469598103934665603
	for _, r := range rows {
		for i := range 4 {
			h ^= uint64(byte(r >> (8 * i)))
			h *= 1099511628211
		}
	}
	return
}

// column is the metric over the current graph, computed once per key and
// cached. ok is false when there is no graph, when the metric needs a seed set
// and none is declared, or when the engine refused.
func (inst *graphviewMetrics) column(m algo.MetricE) (c algo.MetricColumn, ok bool) {
	if inst.g == nil || !m.IsValid() {
		return
	}
	if inst.undirected {
		// Under an undirected reading a direction-bearing metric collapses
		// onto its sibling rather than being refused; the engine owns that
		// mapping (ADR-0232 §SD6).
		m = m.Symmetric()
	}
	key := graphviewMetricKey{metric: m, undirected: inst.undirected}
	if m.IsSeeded() {
		if len(inst.seeds) == 0 {
			return // nothing selected: "near nothing" is not a number
		}
		key.seed = inst.seedKey
	}
	if got, hit := inst.cols[key]; hit {
		return got, true
	}
	got, err := algo.Compute(context.Background(), inst.eng, inst.g, m,
		algo.ComputeOptions{Sources: inst.seeds})
	if err != nil {
		return
	}
	inst.cols[key] = got
	if got.Truncated {
		inst.truncated[m] = got.Truncation
	}
	return got, true
}

// truncatedMetrics names the computed metrics that stopped short, for the
// status line: a clique size under the listing cap is a lower bound and a
// reader sizing nodes by it should know (ADR-0231 §SD6).
func (inst *graphviewMetrics) truncatedMetrics() (out []string) {
	for m := range inst.truncated {
		out = append(out, m.String())
	}
	slices.Sort(out)
	return
}

// graphviewSelector is one encoding selector resolved against a model
// (ADR-0231 §SD6): either a metric the engine computes or a column of the
// contract, optionally inverted.
type graphviewSelector struct {
	Metric algo.MetricE
	// Column names a contract column when the selector is not a metric.
	Column string
	// Invert reads a leading '-': the nearer the brighter, the spelling
	// `ORDER BY -x` already has.
	Invert bool
}

// IsZero reports the unset selector — the channel keeps its conventional
// column.
func (inst graphviewSelector) IsZero() bool {
	return inst.Metric == algo.MetricNone && inst.Column == ""
}

// graphviewChannelE names the encoding channel a selector drives, which is
// what decides whether a categorical metric is allowed.
type graphviewChannelE uint8

const (
	graphviewChannelSize graphviewChannelE = iota
	graphviewChannelTone
	graphviewChannelOpacity
	graphviewChannelAura
)

func (inst graphviewChannelE) String() string {
	switch inst {
	case graphviewChannelTone:
		return "tone_by"
	case graphviewChannelOpacity:
		return "opacity_by"
	case graphviewChannelAura:
		return "aura_by"
	}
	return "size_by"
}

// wantsQuantity reports whether the channel needs an ordinal value. Size and
// opacity are ramps over a magnitude; tone and aura group by distinct value,
// which a label does as well as a number.
func (inst graphviewChannelE) wantsQuantity() bool {
	return inst == graphviewChannelSize || inst == graphviewChannelOpacity
}

// parseGraphviewSelector resolves a selector string. The metric vocabulary is
// the engine's — this keeps no table of its own (ADR-0231 §SD6), so the
// refusals are the parser saying no and the metric's own Kind.
//
// reason is the empty string on success and a sentence for the status line
// otherwise; the caller then leaves the channel on its conventional column.
func parseGraphviewSelector(raw string, ch graphviewChannelE, hasColumn func(string) bool) (sel graphviewSelector, reason string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return
	}
	if rest, cut := strings.CutPrefix(s, "-"); cut {
		sel.Invert = true
		s = strings.TrimSpace(rest)
	}
	if m, ok := algo.ParseMetric(s); ok {
		if ch.wantsQuantity() && m.Kind() == algo.KindCategorical {
			return graphviewSelector{}, "`" + ch.String() + " = " + raw + "`: " + s +
				" is a label rather than a quantity, so it can colour or group but not size"
		}
		sel.Metric = m
		return
	}
	if hasColumn != nil && hasColumn(s) {
		sel.Column = s
		return
	}
	return graphviewSelector{}, "`" + ch.String() + " = " + raw + "`: no column or metric of that name"
}
