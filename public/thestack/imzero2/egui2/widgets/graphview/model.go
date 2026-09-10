package graphview

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// LayoutE selects the node-placement algorithm. The values mirror the
// `Graph` binding's GraphLayoutE so a consumer can cast across.
type LayoutE uint8

const (
	LayoutRandom          LayoutE = 0
	LayoutForceDirected   LayoutE = 1 // Fruchterman–Reingold
	LayoutForceDirectedCG LayoutE = 2 // Fruchterman–Reingold plus centre gravity
	LayoutHierarchical    LayoutE = 3
)

// IsAnimated reports whether the layout advances every frame.
func (inst LayoutE) IsAnimated() bool {
	return inst == LayoutForceDirected || inst == LayoutForceDirectedCG
}

// OrientationE is the growth direction of the hierarchical layout.
type OrientationE uint8

const (
	OrientationTopDown   OrientationE = 0
	OrientationLeftRight OrientationE = 1
)

// EventKindE discriminates [Event]. The numbering is the `Graph` binding's
// GraphEventKindE so a migrating consumer's switch keeps its cases.
type EventKindE uint8

const (
	EventKindNodeClick       EventKindE = 1
	EventKindNodeDoubleClick EventKindE = 2
	EventKindNodeSelect      EventKindE = 3
	EventKindNodeDeselect    EventKindE = 4
	EventKindNodeDragStart   EventKindE = 5
	EventKindNodeDragEnd     EventKindE = 6
	EventKindNodeHoverEnter  EventKindE = 7
	EventKindNodeHoverLeave  EventKindE = 8
	EventKindEdgeClick       EventKindE = 9
	EventKindEdgeSelect      EventKindE = 10
	EventKindEdgeDeselect    EventKindE = 11
)

// IsNode reports whether the kind refers to a node.
func (inst EventKindE) IsNode() bool {
	return inst >= EventKindNodeClick && inst <= EventKindNodeHoverLeave
}

// IsEdge reports whether the kind refers to an edge.
func (inst EventKindE) IsEdge() bool {
	return inst >= EventKindEdgeClick && inst <= EventKindEdgeDeselect
}

// Event is one interaction reported by [View.Events]. Node carries the node
// id for node kinds; From/To carry the edge for edge kinds.
type Event struct {
	Kind EventKindE
	Node uint64
	From uint64
	To   uint64
}

// NodeSpec is one node of the frame's declaration. A zero Color or Radius
// takes the style default.
type NodeSpec struct {
	Id     uint64
	Label  string
	Color  color.Color
	Radius float32 // world units
	Donut  Donut   // a ring of proportional slices around the node; zero draws none
}

// EdgeSpec is one directed edge of the frame's declaration. Parallel edges
// between the same ordered pair are drawn as curves of increasing bulge; a
// From == To edge is a self-loop. A zero Color or Width takes the style
// default.
type EdgeSpec struct {
	From  uint64
	To    uint64
	Label string
	Color color.Color
	Width float32 // screen pixels
}

// ForceParams tunes the Fruchterman–Reingold step. Zero fields take the
// defaults below, which are egui_graphs' — a consumer's tuned values keep
// their meaning. Paused freezes the simulation without discarding it; the
// zero value runs.
type ForceParams struct {
	Dt            float32 // integration step, default 0.05
	Damping       float32 // velocity damping, default 0.3
	Epsilon       float32 // settle threshold on the average displacement, default 1e-3
	MaxStep       float32 // per-node displacement clamp in world units, default 10
	KScale        float32 // scales the ideal edge length k = sqrt(area/n), default 1
	CAttract      float32 // attraction strength, default 1
	CRepulse      float32 // repulsion strength, default 1
	CenterGravity float32 // pull toward the canvas centre, LayoutForceDirectedCG only, default 0.3
	// Theta is the Barnes–Hut opening angle used above a few hundred nodes:
	// smaller is closer to the exact sum and slower. Default 0.9.
	Theta float32
	// Exact forces the O(n²) pair sum at any size — for comparison and
	// tests, not for graphs a user waits on.
	Exact  bool
	Paused bool
}

func (inst ForceParams) withDefaults() ForceParams {
	def := func(v *float32, d float32) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&inst.Dt, 0.05)
	def(&inst.Damping, 0.3)
	def(&inst.Epsilon, 1e-3)
	def(&inst.MaxStep, 10)
	def(&inst.KScale, 1)
	def(&inst.CAttract, 1)
	def(&inst.CRepulse, 1)
	def(&inst.CenterGravity, 0.3)
	def(&inst.Theta, defaultTheta)
	return inst
}

// HierParams tunes the hierarchical layout. RowDist steps between levels
// and ColDist between siblings, both in world units; zero takes 50.
type HierParams struct {
	RowDist      float32
	ColDist      float32
	CenterParent bool // place a parent over the span of its children
	Orientation  OrientationE
}

func (inst HierParams) withDefaults() HierParams {
	if inst.RowDist <= 0 {
		inst.RowDist = 50
	}
	if inst.ColDist <= 0 {
		inst.ColDist = 50
	}
	return inst
}

// Options configures a [View]. The zero value is a random layout with
// dragging, hover, pan and zoom on, no selection, and the default style —
// the `Graph` binding's defaults. Fields are read every frame, so a caller
// may change them between renders.
type Options struct {
	Layout LayoutE
	Force  ForceParams
	Hier   HierParams

	NoDragging         bool // a drag on a node pans instead of moving it
	NoHover            bool // no hover highlight or hover events
	NoZoomAndPan       bool // wheel and background drag are inert
	NodeClicking       bool // emit node click / double-click events
	NodeSelection      bool // a node click toggles selection
	NodeSelectionMulti bool // selection accumulates instead of replacing
	EdgeClicking       bool
	EdgeSelection      bool
	EdgeSelectionMulti bool

	// FitToScreen re-fits the camera every frame. Off, the camera fits once
	// while a freshly laid-out graph settles and then latches off so manual
	// pan and zoom stick (ADR-0224 §SD4); FitNow and ResetLayout re-arm it.
	FitToScreen bool
	FitPadding  float32 // fraction of the canvas kept clear on fit, default 0.1
	// LabelsAlways paints every node label; off, only hovered, selected and
	// dragged nodes carry one.
	LabelsAlways bool

	Style Style
}

// Style holds colours and metrics. A zero Style is DefaultStyle().
type Style struct {
	Background     color.Color
	NodeFill       color.Color // default when a NodeSpec carries none
	NodeStroke     color.Color
	LabelColor     color.Color
	EdgeColor      color.Color // default when an EdgeSpec carries none
	EdgeLabelColor color.Color
	Highlight      color.Color // hovered node or edge
	Selected       color.Color // selected node or edge

	NodeRadius        float32     // world units, default 5
	NodeStrokeW       float32     // screen pixels; 0 (the default) draws no per-node outline, which keeps a node one batched marker
	EdgeWidth         float32     // screen pixels, default 1.5
	TipSize           float32     // arrow head length in screen pixels, default 10
	LabelFontSize     float32     // screen points, default 12 (ADR-0224 §SD7)
	EdgeLabelFontSize float32     // screen points, default 10
	CurveSize         float32     // bulge per parallel-edge order in world units, default 20
	LoopSize          float32     // self-loop radius as a multiple of the node radius, default 3
	DonutWidth        float32     // ring thickness in screen pixels, default 5
	DonutTrack        color.Color // the unfilled remainder when Donut.Total exceeds the values
	Monospace         bool
}

// DefaultStyle returns the design-token default appearance.
func DefaultStyle() Style {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	return Style{
		Background:        hex(styletokens.NeutralBgPanel),
		NodeFill:          hex(styletokens.AccentDefault),
		NodeStroke:        hex(styletokens.NeutralBorderDefault),
		LabelColor:        hex(styletokens.NeutralTextPrimary),
		EdgeColor:         hex(styletokens.NeutralBorderDefault),
		EdgeLabelColor:    hex(styletokens.NeutralTextSecondary),
		Highlight:         hex(styletokens.NeutralTextPrimary),
		Selected:          hex(styletokens.WarningDefault),
		NodeRadius:        5,
		EdgeWidth:         1.5,
		TipSize:           10,
		LabelFontSize:     12,
		EdgeLabelFontSize: 10,
		CurveSize:         20,
		LoopSize:          3,
		DonutWidth:        5,
		DonutTrack:        hex(styletokens.NeutralBorderDefault),
	}
}

// withDefaults fills every unset colour and non-positive metric from
// DefaultStyle, so a caller may set the few fields it cares about.
// NodeStrokeW is the exception: its default is 0.
func (inst Style) withDefaults() Style {
	d := DefaultStyle()
	col := func(v *color.Color, dv color.Color) {
		if isUnset(*v) {
			*v = dv
		}
	}
	num := func(v *float32, dv float32) {
		if *v <= 0 {
			*v = dv
		}
	}
	col(&inst.Background, d.Background)
	col(&inst.NodeFill, d.NodeFill)
	col(&inst.NodeStroke, d.NodeStroke)
	col(&inst.LabelColor, d.LabelColor)
	col(&inst.EdgeColor, d.EdgeColor)
	col(&inst.EdgeLabelColor, d.EdgeLabelColor)
	col(&inst.Highlight, d.Highlight)
	col(&inst.Selected, d.Selected)
	col(&inst.DonutTrack, d.DonutTrack)
	num(&inst.NodeRadius, d.NodeRadius)
	num(&inst.DonutWidth, d.DonutWidth)
	num(&inst.EdgeWidth, d.EdgeWidth)
	num(&inst.TipSize, d.TipSize)
	num(&inst.LabelFontSize, d.LabelFontSize)
	num(&inst.EdgeLabelFontSize, d.EdgeLabelFontSize)
	num(&inst.CurveSize, d.CurveSize)
	num(&inst.LoopSize, d.LoopSize)
	return inst
}

// Metrics is the per-frame readback the settle logic of a consumer needs.
// Steps and LastDisplacement are meaningful for the force layouts only;
// LastDisplacement is NaN before the first step.
type Metrics struct {
	NodeCount        uint32
	EdgeCount        uint32
	Steps            uint64
	LastDisplacement float32
}

var nan32 = float32(math.NaN())

func isUnset(c color.Color) bool { return c.Kind() == color.ColorKindNone }
