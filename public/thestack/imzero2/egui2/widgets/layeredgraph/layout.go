// Package layeredgraph defines the engine-neutral seam for static, layered
// (hierarchical / Sugiyama) layout of directed flow graphs — state machines,
// DAGs, the leeway pipeline. It declares the graph a caller supplies and the
// positioned geometry an engine returns; concrete layout engines (e.g.
// ./goccyengine) live behind the Engine interface.
//
// This is option C from ADR-0069: layout runs host-side, only coordinates
// cross the FFI, and the imzero2 painter draws them. Keeping the contract
// here — independent of any layout backend — is what lets the backend be
// swapped without touching the widget, the FFI payload or the painter.
//
// Coordinates in the result are in the imzero2 painter's space: pixels (equal
// to Graphviz points, 1/72 inch), top-left origin, y increasing downward. The
// caller is free to fit/scale/translate them; nothing here assumes a viewport.
package layeredgraph

import (
	"context"
	"math"
)

// NodeShape is the drawn boundary of a node, rendered by the imzero2 painter.
type NodeShape uint8

const (
	// NodeShapeBox is a (rounded) rectangle — the natural default for states
	// and boxes.
	NodeShapeBox NodeShape = iota
	// NodeShapeCircle is a circle.
	NodeShapeCircle
	// NodeShapeEllipse is an oval — Graphviz's natural node shape, which fits a
	// label more compactly than a circle.
	NodeShapeEllipse
)

// RankDir is the primary flow direction of the layered layout: the axis along
// which ranks (levels) are stacked.
type RankDir uint8

const (
	// RankDirTopBottom stacks ranks top→bottom (Graphviz "TB"), the default.
	RankDirTopBottom RankDir = iota
	// RankDirLeftRight stacks ranks left→right (Graphviz "LR").
	RankDirLeftRight
	// RankDirBottomTop stacks ranks bottom→top (Graphviz "BT").
	RankDirBottomTop
	// RankDirRightLeft stacks ranks right→left (Graphviz "RL").
	RankDirRightLeft
)

// Node is one vertex of the input graph. ID is caller-assigned, must be
// unique, and is echoed back on the matching NodeLayout so the caller can
// correlate geometry with its own model.
type Node struct {
	ID    string
	Label string // drawn text; empty means the ID is used
	Shape NodeShape
	// Weight is an optional ORDINAL quantity carried by this node, read the
	// same way as Edge.Weight: it orders and emphasises, and 0 means
	// *unknown* rather than *none* (ADR-0167 §SD1).
	//
	// Unlike an edge's, a node's weight DOES reach layout: it scales the font
	// its label is laid out at (LayoutOpts.NodeFontSize), and the engine then
	// fits the box to the scaled label as it always has. So a weighted graph
	// and its weightless twin do not lay out identically, and a caller that
	// caches layouts must key on the weights.
	Weight float64
}

// Edge is one directed arc. From and To reference Node.ID values. v1 assumes
// at most one edge per ordered (From, To) pair — consistent with the existing
// `graph` widget's no-multigraph contract; parallel edges collapse to one.
type Edge struct {
	From, To string
	Label    string
	// Weight is an optional ORDINAL quantity carried by this edge: it orders
	// and emphasises, and nothing here treats it as conserved or as comparable
	// across drawings (ADR-0167 §SD1). 0 means *unknown*, not *none* — a
	// renderer must not draw an unweighted edge as the thinnest one.
	//
	// The layout passes it through untouched: it does not influence rank
	// assignment, ordering or routing, so a weighted graph and its weightless
	// twin lay out identically. Rendering it is the view's business
	// (view.RenderOpts.EdgeWidth).
	Weight float64
}

// GraphModel is the directed graph to lay out.
type GraphModel struct {
	Nodes []Node
	Edges []Edge
}

// LayoutOpts tunes the layout. A zero value means "engine default" for each
// field, so the zero LayoutOpts is a sensible top-down layout.
type LayoutOpts struct {
	RankDir  RankDir
	RankSep  float64 // inches between adjacent ranks (levels); 0 = engine default
	NodeSep  float64 // inches between nodes within a rank;       0 = engine default
	FontSize float64 // points, used to size node labels;         0 = engine default
	// NodeFontSize maps a node's ordinal Weight to the font size its label is
	// laid out at, in points. Returning ok=false — or leaving this nil — keeps
	// FontSize, so an unweighted graph is laid out exactly as before.
	//
	// It is a caller-supplied mapping for the same reason the edge width is
	// (ADR-0167 §SD1/§SD3): only the caller knows what its numbers mean.
	// WeightFontSize is a ready-made one. It lives here rather than in the
	// view because a node's size has to be decided before layout runs, where
	// an edge's width is chosen when the edge is stroked.
	NodeFontSize func(weight float64) (pt float64, ok bool)
}

// Magnitude font sizes, in points. The minimum is Graphviz's own default node
// font size, so the lightest carrying node looks like an ordinary one.
const (
	DefaultWeightMinPt = 14.0
	DefaultWeightMaxPt = 34.0
)

// WeightFontSize is a ready-made LayoutOpts.NodeFontSize: it maps each node's
// ordinal weight onto [minPt, maxPt] against the heaviest node in m. Pass 0
// for either bound to take the default above.
//
// The curve, the unknown-vs-none reading and the decline-everything case are
// the same three judgements view.WeightWidth makes, for the same reasons; see
// its doc comment. One difference is worth stating: a font size has a floor
// the eye imposes rather than the design imposes, so minPt is Graphviz's
// default rather than something smaller. Nodes below the mid-range therefore
// order less finely than edges do — they cannot shrink, only fail to grow.
func WeightFontSize(m GraphModel, minPt float64, maxPt float64) func(weight float64) (float64, bool) {
	if minPt <= 0 {
		minPt = DefaultWeightMinPt
	}
	if maxPt <= 0 {
		maxPt = DefaultWeightMaxPt
	}
	if maxPt < minPt {
		maxPt = minPt
	}
	maxV := 0.0
	for i := range m.Nodes {
		if v := m.Nodes[i].Weight; v > maxV {
			maxV = v
		}
	}
	return func(weight float64) (float64, bool) {
		if maxV <= 0 || weight <= 0 {
			return 0, false
		}
		t := math.Sqrt(math.Min(weight, maxV) / maxV)
		return minPt + (maxPt-minPt)*t, true
	}
}

// Point is a 2-D coordinate in the output space (see the package doc):
// pixels, top-left origin, y increasing downward.
type Point struct{ X, Y float64 }

// NodeLayout is a placed node. Center is the node centre; W and H are its full
// bounding-box width and height.
type NodeLayout struct {
	ID     string
	Label  string
	Shape  NodeShape
	Center Point
	W, H   float64
	// FontSize is the size this node's label was laid out at, in points, when
	// it differs from Layout.FontSize — i.e. when a weight scaled it. 0 means
	// the layout-wide size applies, which is the case for every node of an
	// unweighted graph. A renderer paints the label at this size when set, for
	// the same reason it honours Layout.FontSize: the box was fitted to it.
	FontSize float64
}

// EdgeLayout is a routed edge. Points are the control points of the routed
// spline as Graphviz produces it: 1 + 3k points, i.e. a start point followed
// by groups of three defining successive cubic Bézier segments (so a painter
// emits one cubic per group of three after the first). ArrowHead, when
// non-nil, is the point the head arrow's tip lands on. LabelPos, when non-nil,
// is the anchor for Label.
type EdgeLayout struct {
	From, To  string
	Label     string
	Points    []Point
	ArrowHead *Point
	LabelPos  *Point
	// Weight is the input Edge.Weight, carried through untouched so a renderer
	// can map it without holding the input model alongside the layout.
	Weight float64
}

// Layout is the positioned result. Width and Height are the overall bounding
// box; every coordinate produced falls within [0,Width] × [0,Height].
type Layout struct {
	Nodes  []NodeLayout
	Edges  []EdgeLayout
	Width  float64
	Height float64
	// FontSize is the node-label font size (points) the engine sized the boxes
	// to fit. A renderer should paint node labels at this size (before its own
	// fit scale) so the text matches the boxes — this is what keeps the layout
	// font and the render font from drifting apart. 0 means the engine reported
	// none (e.g. a hand-built Layout); a renderer then falls back to its style.
	FontSize float64
}

// Engine computes a static layered layout for a GraphModel. Implementations
// are not required to be safe for concurrent use — callers serialise (the
// imzero2 render loop is single-threaded). ctx bounds the call (engines may
// run a WebAssembly runtime under the hood). See ADR-0069 for the rationale
// behind this seam.
type Engine interface {
	Layout(ctx context.Context, model GraphModel, opts LayoutOpts) (*Layout, error)
}
