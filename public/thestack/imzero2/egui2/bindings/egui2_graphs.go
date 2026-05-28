//go:build llm_generated_opus47

package bindings

// GraphLayoutE selects the node-placement algorithm used by a Graph
// widget. Mirror of the GRAPH_LAYOUT_* constants in Rust. Note that
// switching layout at runtime discards the previous layout's positions
// (different egui-state types occupy the same storage slot), so treat
// it as a per-widget constant in practice.
type GraphLayoutE uint8

const (
	GraphLayoutRandom          GraphLayoutE = 0
	GraphLayoutForceDirected   GraphLayoutE = 1 // Fruchterman-Reingold
	GraphLayoutForceDirectedCG GraphLayoutE = 2 // Fruchterman-Reingold + center gravity
	GraphLayoutHierarchical    GraphLayoutE = 3
)

// GraphHierarchicalOrientationE picks the layout direction of the
// hierarchical algorithm. Mirror of egui_graphs::LayoutHierarchicalOrientation.
type GraphHierarchicalOrientationE uint8

const (
	GraphHierarchicalOrientationTopDown   GraphHierarchicalOrientationE = 0
	GraphHierarchicalOrientationLeftRight GraphHierarchicalOrientationE = 1
)

// GraphEventKindE discriminates the variants of GraphEvent. Mirror of the
// GRAPH_EV_* constants in src/rust/src/imzero2/interpreter.rs; change both
// in lockstep if the set evolves. Pan/Zoom/NodeMove are intentionally
// omitted in v1 — they're continuous per-frame streams.
type GraphEventKindE uint8

const (
	GraphEventKindNodeClick       GraphEventKindE = 1
	GraphEventKindNodeDoubleClick GraphEventKindE = 2
	GraphEventKindNodeSelect      GraphEventKindE = 3
	GraphEventKindNodeDeselect    GraphEventKindE = 4
	GraphEventKindNodeDragStart   GraphEventKindE = 5
	GraphEventKindNodeDragEnd     GraphEventKindE = 6
	GraphEventKindNodeHoverEnter  GraphEventKindE = 7
	GraphEventKindNodeHoverLeave  GraphEventKindE = 8
	GraphEventKindEdgeClick       GraphEventKindE = 9
	GraphEventKindEdgeSelect      GraphEventKindE = 10
	GraphEventKindEdgeDeselect    GraphEventKindE = 11
)

// GraphEvent is one interaction event for a specific Graph widget. KeyA
// is the node id for node events (kind 1..=8) or the edge-source id for
// edge events (kind 9..=11); KeyB is 0 for node events and the edge-target
// id for edge events.
type GraphEvent struct {
	GraphId uint64
	Kind    GraphEventKindE
	KeyA    uint64
	KeyB    uint64
}

// IsNode reports whether this event refers to a node (KeyA is the node id).
func (inst GraphEvent) IsNode() bool { return inst.Kind >= 1 && inst.Kind <= 8 }

// IsEdge reports whether this event refers to an edge (KeyA=from, KeyB=to).
func (inst GraphEvent) IsEdge() bool { return inst.Kind >= 9 && inst.Kind <= 11 }

// GraphSelectedItem is one entry in the current selection snapshot. When
// IsNode is true, KeyA is the node id and KeyB is 0; when false, KeyA is
// the edge source and KeyB is the edge target.
type GraphSelectedItem struct {
	GraphId uint64
	IsNode  bool
	KeyA    uint64
	KeyB    uint64
}

// FetchGraphSelection returns the previous frame's per-graph selection
// snapshot, drained and decoded at frame-end by StateManager.Sync. The
// snapshot is rebuilt every frame from the Rust side (selected() on
// each node/edge), so stale selections don't accumulate.
//
// The slice is owned by the StateManager and reused next frame; copy
// before retaining past this frame. See FetchSnarlEvents for the
// deferred-capture deadlock rationale.
func FetchGraphSelection() []GraphSelectedItem {
	return CurrentApplicationState.StateManager.GetGraphSelection()
}

// GraphMetrics is one row of the per-graph metrics snapshot. FrSteps and
// FrLastDisplacement are meaningful only when the graph's layout was FR
// or FR+CG; otherwise they are 0 / NaN.
type GraphMetrics struct {
	GraphId            uint64
	NodeCount          uint32
	EdgeCount          uint32
	FrSteps            uint64
	FrLastDisplacement float32
}

// FetchGraphMetrics returns per-graph metrics (node/edge counts, FR
// step counter, last avg displacement), drained at frame-end by
// StateManager.Sync. One row per graph widget rendered last frame.
// See FetchSnarlEvents for the deferred-capture deadlock rationale.
func FetchGraphMetrics() []GraphMetrics {
	return CurrentApplicationState.StateManager.GetGraphMetrics()
}

// FetchGraphEvents returns the previous frame's egui_graphs interaction
// events, drained and decoded at frame-end by StateManager.Sync. The
// slice is owned by the StateManager and reused next frame; copy
// before retaining past this frame. See FetchSnarlEvents for the
// deferred-capture deadlock rationale.
func FetchGraphEvents() []GraphEvent {
	return CurrentApplicationState.StateManager.GetGraphEvents()
}
