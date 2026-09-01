// Package sankey models and lays out flow-quantity diagrams: nodes arranged
// in stages, joined by ribbons whose thickness is proportional to a conserved
// value (ADR-0159). It covers two modes — Sankey, where the stage index is
// derived from the graph and the order within a stage is relaxed to reduce
// crossings, and alluvial, where the caller fixes both.
//
// The package is UI-free. This file declares the model and its validation;
// layout.go turns a Diagram into positioned geometry; ./view — the only half
// that imports the egui2 bindings — draws that geometry as implot custom
// items. The split mirrors layeredgraph (ADR-0069) and pipelineview
// (ADR-0119).
//
// Geometry is emitted in a unit box: x and y both in [0,1], y in plot
// convention (larger y is higher on screen), so a renderer can pin an implot
// plot to [0,1] on both axes and project through the frame transform. Pan and
// zoom are then implot's job, not the widget's (ADR-0159 SD1).
package sankey

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Mode selects how a diagram's stage index and within-stage order are
// decided. The two modes share one layout pipeline — the global value scale,
// far-end-y link ordering, collision resolution and ribbon geometry are
// identical — and differ only in those two switches (ADR-0159 SD4).
type Mode uint8

const (
	// ModeSankey derives the stage index by longest path from the sources and
	// relaxes the order within a stage to reduce crossings. Links may span
	// any number of stages.
	ModeSankey Mode = iota
	// ModeAlluvial takes the stage index from Node.Stage and orders each
	// stage by Node.Order (ties broken by descending value, then id), which
	// keeps a category in the same relative place across stages. Links must
	// join adjacent stages.
	ModeAlluvial
)

// Align selects where nodes that are free to move land, in ModeSankey only.
// It is the classic four-way choice: a node with no outgoing links can sit
// where its depth puts it, or be pushed to the last stage.
type Align uint8

const (
	// AlignJustify pushes sinks (no outgoing links) to the last stage. It is
	// the default because a flow diagram usually wants its terminal
	// quantities lined up on the right edge.
	AlignJustify Align = iota
	// AlignLeft places every node at its longest-path depth from a source.
	AlignLeft
	// AlignRight places every node as late as it can go: the last stage minus
	// its longest path to a sink.
	AlignRight
	// AlignCenter is AlignLeft, except that a node with no incoming links is
	// pulled right to sit just before its earliest target.
	AlignCenter
)

// Node is one vertex: a stage-resident bar whose height is its value, which
// is the larger of the flow entering and the flow leaving it.
type Node struct {
	// ID is unique within the diagram and is what Link.Source / Link.Target
	// name. It is also the tie-breaker that keeps layout deterministic.
	ID string
	// Label is what the renderer draws; empty falls back to ID.
	Label string
	// Stage is the column index, required in ModeAlluvial (>= 0) and ignored
	// in ModeSankey, where it is derived.
	Stage int
	// Order is the sort key within a stage in ModeAlluvial; ignored in
	// ModeSankey. Equal keys fall back to descending value, then ID.
	Order float64
	// Color is 0xRRGGBBAA. Zero defers to the renderer's palette.
	Color uint32
}

// Link is one flow of Value units from Source to Target, both Node IDs.
type Link struct {
	Source string
	Target string
	// Value is the quantity carried, strictly positive. It sets the ribbon's
	// thickness through the diagram-wide scale, never a per-stage one.
	Value float64
	// Label is optional annotation for the renderer (a tooltip, typically).
	Label string
	// Color is 0xRRGGBBAA. Zero defers to the renderer, which by default
	// tints a ribbon from its endpoints.
	Color uint32
}

// Diagram is the whole input: nodes, links, the mode, and the unit the values
// are counted in. Mixing units in one diagram is the classic Sankey lie; Unit
// is carried through to the Report so a host can label the total and make the
// choice visible rather than implicit.
type Diagram struct {
	Nodes []Node
	Links []Link
	Mode  Mode
	Unit  string
}

// Options tunes the layout. The zero value is usable: every field falls back
// to the constant beside it.
//
// The three box fractions take the default not only at zero but at any value
// a fraction cannot be — negative, NaN, infinite. Laying a diagram out with a
// negative bar width would draw inside-out bars rather than report the
// mistake, and a silently wrong picture is the one failure this form cannot
// afford. Iterations keeps its own rule, below.
type Options struct {
	// Iterations is the number of barycentre relaxation sweeps, ModeSankey
	// only. 0 means DefaultIterations; a negative value disables relaxation,
	// leaving the insertion order.
	Iterations int
	// NodePad is the vertical gap between two nodes in a stage, as a fraction
	// of the box height. 0 means DefaultNodePad. It shrinks when a stage has
	// too many nodes to pad them all.
	NodePad float64
	// NodeWidth is the bar's thickness, as a fraction of the box width.
	// 0 means DefaultNodeWidth. It is capped so that stages cannot overlap,
	// however many of them there are; Layout.NodeWidth reports what was used.
	NodeWidth float64
	// Align places nodes that are free to move (ModeSankey only).
	Align Align
	// ThinFrac is the ribbon-thickness fraction below which a link is counted
	// in Report.ThinLinks — the sub-pixel-flow warning. 0 means
	// DefaultThinFrac.
	ThinFrac float64
}

// Layout defaults. They are exported because a caller reproducing the
// geometry (a hit test in a host, say) needs the same numbers.
const (
	DefaultIterations = 6
	DefaultNodePad    = 0.02
	DefaultNodeWidth  = 0.02
	DefaultThinFrac   = 0.002
)

func (o Options) withDefaults() Options {
	if o.Iterations == 0 {
		o.Iterations = DefaultIterations
	} else if o.Iterations < 0 {
		o.Iterations = 0
	}
	o.NodePad = frac(o.NodePad, DefaultNodePad)
	o.NodeWidth = frac(o.NodeWidth, DefaultNodeWidth)
	o.ThinFrac = frac(o.ThinFrac, DefaultThinFrac)
	return o
}

// frac resolves one of Options' box fractions: anything that is not a finite
// positive number asks for the default. The comparison rejects NaN too, which
// is why it is written as a positive test rather than a chain of negations.
func frac(v float64, def float64) float64 {
	if v > 0 && !math.IsInf(v, 1) {
		return v
	}
	return def
}

// Report records what the layout noticed but could not decide on the
// caller's behalf: quantities that do not balance, and flows too thin to
// read. It is advisory — nothing in it stops a diagram from rendering.
type Report struct {
	// Unit is Diagram.Unit, carried through for labelling.
	Unit string
	// Total is the quantity the diagram carries: the outflow of every node
	// that has no inflow.
	//
	// It is deliberately not the sum of all link values. A flow crossing
	// three stages is three links but one quantity, so that sum overstates a
	// conserved diagram by roughly its stage count — 199 for an energy
	// balance of 80 — and it is wrong in the direction that flatters, which
	// is the direction a caller is least likely to check. It is also the
	// denominator a host reaches for when writing "x% of total"; getting it
	// wrong there misstates every share on screen.
	Total float64
	// NonConserving names nodes with both inflow and outflow that disagree by
	// more than a relative epsilon. Sources and sinks are not listed: their
	// imbalance is what makes them sources and sinks.
	NonConserving []string
	// ThinLinks counts links whose ribbon is thinner than Options.ThinFrac of
	// the box. They are drawn at their true thickness, so they may be
	// invisible; a host that cares should aggregate them upstream.
	ThinLinks int
}

// conserveEps is the relative tolerance for the in-vs-out balance check.
const conserveEps = 1e-9

// Validate reports the first structural problem with d, or nil. Compute calls
// it, so calling it separately is only useful to check input before laying
// out — for instance to show a host's own error state.
//
// Cycles are rejected rather than drawn (ADR-0159 SD6): a stage-ordered
// diagram cannot show a flow that returns to an earlier stage, and silently
// dropping the offending edge would misstate the quantities.
func (d Diagram) Validate() error {
	_, _, err := d.validate()
	return err
}

// validate is Validate's working half. It hands back what the checking had to
// build anyway — the id-to-index map, and in ModeSankey a topological order —
// so Compute does not build either a second time.
//
// order is empty in ModeAlluvial: the stage index is given there, and the
// adjacency rule already rules a cycle out, so nothing needs a traversal.
func (d Diagram) validate() (index map[string]int, order []int, err error) {
	if len(d.Nodes) == 0 {
		return nil, nil, eh.Errorf("diagram has no nodes")
	}
	index = make(map[string]int, len(d.Nodes))
	for i, n := range d.Nodes {
		if n.ID == "" {
			return nil, nil, eb.Build().Int("node", i).Errorf("node has an empty id")
		}
		if _, dup := index[n.ID]; dup {
			return nil, nil, eb.Build().Str("id", n.ID).Errorf("duplicate node id")
		}
		if d.Mode == ModeAlluvial && n.Stage < 0 {
			return nil, nil, eb.Build().Str("id", n.ID).Int("stage", n.Stage).Errorf("node has a negative stage; alluvial stages must be >= 0")
		}
		index[n.ID] = i
	}
	for i, l := range d.Links {
		si, okS := index[l.Source]
		if !okS {
			return nil, nil, eb.Build().Int("link", i).Str("source", l.Source).Errorf("link references an unknown source")
		}
		ti, okT := index[l.Target]
		if !okT {
			return nil, nil, eb.Build().Int("link", i).Str("target", l.Target).Errorf("link references an unknown target")
		}
		if si == ti {
			return nil, nil, eb.Build().Int("link", i).Str("source", l.Source).Errorf("link is a self-link")
		}
		if math.IsNaN(l.Value) || math.IsInf(l.Value, 0) || l.Value <= 0 {
			return nil, nil, eb.Build().Int("link", i).Str("source", l.Source).Str("target", l.Target).Float64("value", l.Value).
				Errorf("link values must be finite and > 0")
		}
		if d.Mode == ModeAlluvial {
			if got := d.Nodes[ti].Stage - d.Nodes[si].Stage; got != 1 {
				return nil, nil, eb.Build().Int("link", i).Str("source", l.Source).Str("target", l.Target).Int("stages", got).
					Errorf("alluvial links must join adjacent stages")
			}
		}
	}
	if d.Mode == ModeSankey {
		if order, err = d.topoOrder(index); err != nil {
			return nil, nil, err
		}
	}
	return index, order, nil
}

// topoOrder runs Kahn's algorithm. The settled queue is the topological order,
// so the one traversal answers both questions asked of it: whether a cycle
// survives, and — when none does — the order assignStages sweeps in.
func (d Diagram) topoOrder(index map[string]int) ([]int, error) {
	n := len(d.Nodes)
	indeg := make([]int, n)
	out := make([][]int, n)
	for _, l := range d.Links {
		s, t := index[l.Source], index[l.Target]
		out[s] = append(out[s], t)
		indeg[t]++
	}
	order := make([]int, 0, n)
	for i := range n {
		if indeg[i] == 0 {
			order = append(order, i)
		}
	}
	// The queue grows as it is walked; k catches up with len(order) exactly
	// when nothing is left to settle.
	for k := 0; k < len(order); k++ {
		for _, w := range out[order[k]] {
			indeg[w]--
			if indeg[w] == 0 {
				order = append(order, w)
			}
		}
	}
	if len(order) == n {
		return order, nil
	}
	// Name a concrete edge inside the cycle: both ends are unsettled.
	for i, l := range d.Links {
		if indeg[index[l.Source]] > 0 && indeg[index[l.Target]] > 0 {
			return nil, eb.Build().Int("link", i).Str("source", l.Source).Str("target", l.Target).
				Errorf("link lies on a cycle; flow diagrams are acyclic (ADR-0159 SD6)")
		}
	}
	return nil, eh.Errorf("the graph contains a cycle")
}
