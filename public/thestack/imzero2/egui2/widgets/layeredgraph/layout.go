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

import "context"

// NodeShape is the drawn boundary of a node. v1 is limited to the shapes the
// imzero2 painter already renders without new primitives (ADR-0069): a
// rounded rectangle and a circle. Richer shapes (ellipse, polygon) are
// deferred until the painter grows the matching primitives.
type NodeShape uint8

const (
	// NodeShapeBox is a (rounded) rectangle — the natural default for states
	// and boxes.
	NodeShapeBox NodeShape = iota
	// NodeShapeCircle is a circle.
	NodeShapeCircle
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
}

// Edge is one directed arc. From and To reference Node.ID values. v1 assumes
// at most one edge per ordered (From, To) pair — consistent with the existing
// `graph` widget's no-multigraph contract; parallel edges collapse to one.
type Edge struct {
	From, To string
	Label    string
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
}

// Layout is the positioned result. Width and Height are the overall bounding
// box; every coordinate produced falls within [0,Width] × [0,Height].
type Layout struct {
	Nodes  []NodeLayout
	Edges  []EdgeLayout
	Width  float64
	Height float64
}

// Engine computes a static layered layout for a GraphModel. Implementations
// are not required to be safe for concurrent use — callers serialise (the
// imzero2 render loop is single-threaded). ctx bounds the call (engines may
// run a WebAssembly runtime under the hood). See ADR-0069 for the rationale
// behind this seam.
type Engine interface {
	Layout(ctx context.Context, model GraphModel, opts LayoutOpts) (*Layout, error)
}
