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
	"fmt"
	"math"
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
type Options struct {
	// Iterations is the number of barycentre relaxation sweeps, ModeSankey
	// only. 0 means DefaultIterations; a negative value disables relaxation,
	// leaving the insertion order.
	Iterations int
	// NodePad is the vertical gap between two nodes in a stage, as a fraction
	// of the box height. 0 means DefaultNodePad.
	NodePad float64
	// NodeWidth is the bar's thickness, as a fraction of the box width.
	// 0 means DefaultNodeWidth.
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
	if o.NodePad == 0 {
		o.NodePad = DefaultNodePad
	}
	if o.NodeWidth == 0 {
		o.NodeWidth = DefaultNodeWidth
	}
	if o.ThinFrac == 0 {
		o.ThinFrac = DefaultThinFrac
	}
	return o
}

// Report records what the layout noticed but could not decide on the
// caller's behalf: quantities that do not balance, and flows too thin to
// read. It is advisory — nothing in it stops a diagram from rendering.
type Report struct {
	// Unit is Diagram.Unit, carried through for labelling.
	Unit string
	// Total is the sum of all link values.
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
	if len(d.Nodes) == 0 {
		return fmt.Errorf("sankey: diagram has no nodes")
	}
	seen := make(map[string]int, len(d.Nodes))
	for i, n := range d.Nodes {
		if n.ID == "" {
			return fmt.Errorf("sankey: node %d has an empty id", i)
		}
		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("sankey: duplicate node id %q", n.ID)
		}
		if d.Mode == ModeAlluvial && n.Stage < 0 {
			return fmt.Errorf("sankey: node %q has stage %d; alluvial stages must be >= 0", n.ID, n.Stage)
		}
		seen[n.ID] = i
	}
	for i, l := range d.Links {
		si, okS := seen[l.Source]
		if !okS {
			return fmt.Errorf("sankey: link %d references unknown source %q", i, l.Source)
		}
		ti, okT := seen[l.Target]
		if !okT {
			return fmt.Errorf("sankey: link %d references unknown target %q", i, l.Target)
		}
		if si == ti {
			return fmt.Errorf("sankey: link %d is a self-link on %q", i, l.Source)
		}
		if math.IsNaN(l.Value) || math.IsInf(l.Value, 0) || l.Value <= 0 {
			return fmt.Errorf("sankey: link %d (%s->%s) has value %v; values must be finite and > 0",
				i, l.Source, l.Target, l.Value)
		}
		if d.Mode == ModeAlluvial {
			if got := d.Nodes[ti].Stage - d.Nodes[si].Stage; got != 1 {
				return fmt.Errorf("sankey: link %d (%s->%s) spans %d stages; alluvial links must join adjacent stages",
					i, l.Source, l.Target, got)
			}
		}
	}
	if d.Mode == ModeSankey {
		if err := d.checkAcyclic(seen); err != nil {
			return err
		}
	}
	return nil
}

// checkAcyclic runs Kahn's algorithm and names an edge on a surviving cycle.
func (d Diagram) checkAcyclic(index map[string]int) error {
	n := len(d.Nodes)
	indeg := make([]int, n)
	out := make([][]int, n)
	for _, l := range d.Links {
		s, t := index[l.Source], index[l.Target]
		out[s] = append(out[s], t)
		indeg[t]++
	}
	queue := make([]int, 0, n)
	for i := range n {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	settled := 0
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		settled++
		for _, w := range out[v] {
			indeg[w]--
			if indeg[w] == 0 {
				queue = append(queue, w)
			}
		}
	}
	if settled == n {
		return nil
	}
	// Name a concrete edge inside the cycle: both ends are unsettled.
	for i, l := range d.Links {
		if indeg[index[l.Source]] > 0 && indeg[index[l.Target]] > 0 {
			return fmt.Errorf("sankey: link %d (%s->%s) lies on a cycle; flow diagrams are acyclic (ADR-0159 SD6)",
				i, l.Source, l.Target)
		}
	}
	return fmt.Errorf("sankey: the graph contains a cycle")
}
