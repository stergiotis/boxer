package graphview

import (
	"cmp"
	"iter"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// View is one graph widget instance. Construct it once per graph with [New]
// and keep it across frames; it owns positions, selection, the camera and
// the simulation state (ADR-0224 §SD1). Opts is read every frame.
type View struct {
	Opts Options

	ids     *c.WidgetIdStack
	key     string
	paneKey string

	g     graph
	fs    forceState
	cam   camera
	style Style // Opts.Style with defaults filled, resolved once per Render

	hierDone     bool
	lastHier     HierParams
	everHadNodes bool
	fitPending   bool
	fitFrames    uint32
	fitRequested bool
	resetPending bool
	ffSteps      uint32

	hoveredId   uint64
	hoveredOk   bool
	hoveredEdge int32 // edge index in this frame's edge arrays, or -1
	selNodes    map[uint64]struct{}
	selEdges    map[[2]uint64]struct{}
	drag        dragState

	events   []Event
	originX  float32 // canvas top-left in screen pixels, from the R24 row
	originY  float32
	originOk bool

	// scratch
	batchIdx     map[uint64]int32
	batches      []nodeBatch
	batchXs      []float32
	batchYs      []float32
	tipXs        [3]float32
	tipYs        [3]float32
	selOrder     []uint64
	selEdgeOrder [][2]uint64
	arcs         []donutArc
	arcXs        []float32
	arcYs        []float32
}

// nodeBatch is one PaintMarkers call: every node sharing a colour and a
// radius. Batches are emitted in first-seen order, which follows the
// declaration and so is deterministic frame to frame.
type nodeBatch struct {
	col    color.Color
	radius float32
	slots  []int32
}

type dragState struct {
	active bool
	isNode bool   // moving a node rather than panning
	nodeId uint64 // the node, when isNode
	lastX  float32
	lastY  float32
}

// fitMinFrames is the floor of fitted frames before the one-shot latch may
// release, so a layout that reports settled at once is still framed after
// its first placement.
const fitMinFrames = 8

// New returns a widget whose ids are scoped under key on ids. Two views
// under one stack need distinct keys.
func New(ids *c.WidgetIdStack, key string, opts Options) *View {
	return &View{
		Opts:        opts,
		ids:         ids,
		key:         key,
		paneKey:     key + "-pane",
		cam:         camera{zoom: 1},
		hoveredEdge: -1,
		selNodes:    make(map[uint64]struct{}, 8),
		selEdges:    make(map[[2]uint64]struct{}, 8),
		batchIdx:    make(map[uint64]int32, 8),
		fs:          forceState{lastDisp: nan32},
	}
}

// FitNow re-arms the one-shot fit: the camera frames the graph while the
// layout settles, then latches off again. Fire once, not every frame.
func (v *View) FitNow() { v.fitRequested = true }

// ResetLayout discards every position before the next render: nodes are
// placed afresh by the layout, widget-side pins are released, the step
// counter restarts and the camera re-fits.
func (v *View) ResetLayout() { v.resetPending = true }

// FastForward advances the force simulation by steps extra iterations
// before the next render. A no-op for the static layouts.
func (v *View) FastForward(steps uint32) { v.ffSteps += steps }

// Events returns the interactions the previous frame's input produced,
// valid until the next Render.
func (v *View) Events() []Event { return v.events }

// Metrics returns the counts and the force layout's settle state.
func (v *View) Metrics() Metrics {
	return Metrics{
		NodeCount:        uint32(v.g.n()),
		EdgeCount:        uint32(len(v.g.eFrom)),
		PinnedCount:      v.g.pinnedCount,
		Steps:            v.fs.steps,
		LastDisplacement: v.fs.lastDisp,
		Settled:          v.Opts.Layout.IsAnimated() && v.fs.settled(v.Opts.Force.withDefaults().Epsilon),
	}
}

// IsSettled reports whether the layout has stopped moving for any reason —
// the predicate the fit latch waits on: a static layout always, a paused
// force layout, or a force layout whose average displacement is at or under
// Epsilon. Metrics.Settled is the convergence test alone.
func (v *View) IsSettled() bool {
	if !v.Opts.Layout.IsAnimated() || v.Opts.Force.Paused {
		return true
	}
	return v.fs.settled(v.Opts.Force.withDefaults().Epsilon)
}

// HoveredNode returns the node under the pointer, as of the previous frame.
func (v *View) HoveredNode() (id uint64, ok bool) {
	return v.hoveredId, v.hoveredOk
}

// HoveredEdge returns the edge under the pointer, as of the previous frame,
// when no node is. Edge hover has no events; parallel edges of one ordered
// pair are not told apart.
func (v *View) HoveredEdge() (from, to uint64, ok bool) {
	if e := v.hoveredEdge; e >= 0 && int(e) < len(v.g.eFrom) {
		return v.g.ids[v.g.eFrom[e]], v.g.ids[v.g.eTo[e]], true
	}
	return
}

// SelectedNodes yields the selected node ids in ascending order. The
// iterator shares the view's scratch; iterate it to completion before
// starting another.
func (v *View) SelectedNodes() iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		v.selOrder = v.selOrder[:0]
		for id := range v.selNodes {
			v.selOrder = append(v.selOrder, id)
		}
		slices.Sort(v.selOrder)
		for _, id := range v.selOrder {
			if !yield(id) {
				return
			}
		}
	}
}

// SelectedEdges yields the selected edges as (from, to) pairs, ordered. The
// same scratch caveat as SelectedNodes applies.
func (v *View) SelectedEdges() iter.Seq[[2]uint64] {
	return func(yield func([2]uint64) bool) {
		v.selEdgeOrder = v.selEdgeOrder[:0]
		for k := range v.selEdges {
			v.selEdgeOrder = append(v.selEdgeOrder, k)
		}
		slices.SortFunc(v.selEdgeOrder, func(a, b [2]uint64) int {
			if r := cmp.Compare(a[0], b[0]); r != 0 {
				return r
			}
			return cmp.Compare(a[1], b[1])
		})
		for _, k := range v.selEdgeOrder {
			if !yield(k) {
				return
			}
		}
	}
}

// NodePosition returns a node's world position.
func (v *View) NodePosition(id uint64) (x, y float32, ok bool) {
	s, ok := v.g.slot[id]
	if !ok {
		return
	}
	return v.g.x[s], v.g.y[s], true
}

// NodeCanvasPosition returns where a node was painted in the last Render,
// in canvas pixels from the canvas's top-left. Add CanvasScreenOrigin to
// place an overlay outside the canvas beside it.
func (v *View) NodeCanvasPosition(id uint64) (x, y float32, ok bool) {
	s, ok := v.g.slot[id]
	if !ok {
		return
	}
	x, y = v.cam.toScreen(v.g.x[s], v.g.y[s])
	return x, y, true
}

// CanvasScreenOrigin returns the canvas's top-left in screen pixels as the
// host reported it for the previous frame. ok is false until the canvas has
// rendered once.
func (v *View) CanvasScreenOrigin() (x, y float32, ok bool) {
	return v.originX, v.originY, v.originOk
}

// Camera returns the view transform: canvas = world · zoom + pan.
func (v *View) Camera() (zoom, panX, panY float32) {
	return v.cam.zoom, v.cam.panX, v.cam.panY
}

// SetCamera sets the view transform and releases a pending fit, so the
// caller's framing sticks.
func (v *View) SetCamera(zoom, panX, panY float32) {
	v.cam.zoom = min(max(zoom, minZoom), maxZoom)
	v.cam.panX, v.cam.panY = panX, panY
	v.fitPending = false
}

// CanvasToWorld maps a canvas pixel to world units under the last camera.
func (v *View) CanvasToWorld(x, y float32) (wx, wy float32) {
	return v.cam.toWorld(x, y)
}

// PinNode holds a node at (x, y) in world units until UnpinNode: a
// widget-side pin the force layout leaves alone (ADR-0224 §SD10). A pin
// declared on the NodeSpec takes precedence while it is declared.
func (v *View) PinNode(id uint64, x, y float32) {
	if s, ok := v.g.slot[id]; ok {
		v.g.x[s], v.g.y[s] = x, y
		v.g.held[s] = true
	}
}

// UnpinNode releases a widget-side pin set by PinNode or Options.PinOnDrag.
// A pin declared on the NodeSpec is the caller's to drop.
func (v *View) UnpinNode(id uint64) {
	if s, ok := v.g.slot[id]; ok {
		v.g.held[s] = false
	}
}

// IsPinned reports whether the node is fixed by a declared or a widget-side
// pin.
func (v *View) IsPinned(id uint64) bool {
	s, ok := v.g.slot[id]
	return ok && v.g.isPinned(int(s))
}

// SetNodePosition moves a node in world units; a force layout continues
// from there. A declared pin puts the node back next frame.
func (v *View) SetNodePosition(id uint64, x, y float32) {
	if s, ok := v.g.slot[id]; ok {
		v.g.x[s], v.g.y[s] = x, y
	}
}

// RenderFill is Render sized to the pane the widget sits in, as reported by
// the layout probe one frame late; the fallbacks serve the first frame and
// a hidden tab.
func (v *View) RenderFill(nodes []NodeSpec, edges []EdgeSpec, fallbackW, fallbackH float32) {
	w, h := fallbackW, fallbackH
	if pw, ph, ok := c.CapturePaneSize(v.ids.PrepareStr(v.paneKey).Derive()); ok && pw > 0 && ph > 0 {
		w, h = pw, ph
	}
	v.Render(nodes, edges, w, h)
}

// Render reconciles the declaration, applies the previous frame's input,
// advances the layout and paints into a w×h canvas at the current layout
// position. A non-positive size renders nothing; RenderFill takes the
// pane's size.
func (v *View) Render(nodes []NodeSpec, edges []EdgeSpec, w, h float32) {
	v.events = v.events[:0]
	if w <= 0 || h <= 0 {
		return
	}
	v.style = v.Opts.Style.withDefaults()
	fp := v.Opts.Force.withDefaults()
	hp := v.Opts.Hier.withDefaults()

	for range c.IdScope(v.ids.PrepareStr(v.key)) {
		sm := c.CurrentApplicationState.StateManager
		canvasH := widgethandle.Make(v.ids.PrepareStr("graphview-canvas").Derive())
		areaH := widgethandle.Make(v.ids.PrepareStr("graphview-area").Derive())
		canvasFlags := sm.GetResponse(canvasH)
		areaFlags := sm.GetResponse(areaH)
		wheel := sm.GetCanvasWheel(canvasH)

		// Pointer in canvas pixels: the global pointer against the canvas's
		// R24 origin, refined by the sense region's own row when it has one
		// (press origin on the drag-started frame; drag-stable after).
		px, py, posOk := float32(0), float32(0), false
		if cur, ok := sm.GetCanvasCursor(canvasH); ok {
			v.originX, v.originY, v.originOk = cur.OriginX, cur.OriginY, true
			if ptr := sm.GetPointer(); ptr.Valid {
				px, py, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
			}
		}
		if areaCur, ok := sm.GetCanvasCursor(areaH); ok && !isNaN32(areaCur.PosX) {
			px, py, posOk = areaCur.PosX, areaCur.PosY, true
		}
		inside := posOk && canvasFlags.HasContainsPointer() && px >= 0 && py >= 0 && px <= w && py <= h

		created, topoChanged := v.g.reconcile(nodes, edges)
		if topoChanged {
			v.pruneSelection()
		}
		n := v.g.n()

		if v.resetPending {
			v.resetPending = false
			v.hierDone = false
			v.fs.reset()
			clear(v.g.held)
			created = v.g.allSlots()
			v.drag = dragState{}
			v.fitRequested = true
		}

		// Placement of nodes that have none yet.
		if len(created) > 0 {
			switch v.Opts.Layout {
			case LayoutForceDirected, LayoutForceDirectedCG:
				k := idealEdgeLength(w, h, n, fp.KScale)
				placeNear(&v.g, created, max(k, 1))
			case LayoutHierarchical:
				// laid out below with the rest of the tree
			default:
				placeRandom(&v.g, created)
			}
		}
		if v.Opts.Layout == LayoutHierarchical && (topoChanged || !v.hierDone || len(created) > 0 || hp != v.lastHier) {
			layoutHierarchical(&v.g, hp)
			v.hierDone = true
			v.lastHier = hp
		}
		// Declared pins win over any placement; the drag in flight keeps its
		// node where the pointer has it (ADR-0224 §SD10).
		v.g.applyPins(v.dragSlot())

		// Input, against the previous frame's geometry. A drag that began
		// this frame fixes its node before the step below can move it.
		v.applyInput(w, h, px, py, posOk, inside, areaFlags, wheel)
		if s := v.dragSlot(); s >= 0 {
			v.g.fixed[s] = true
		}

		// Layout.
		if v.Opts.Layout.IsAnimated() {
			cg := float32(0)
			if v.Opts.Layout == LayoutForceDirectedCG {
				cg = fp.CenterGravity
			}
			steps := v.ffSteps
			v.ffSteps = 0
			if !fp.Paused {
				steps++
			}
			for range steps {
				v.fs.step(&v.g, w, h, fp, cg)
			}
		} else {
			v.ffSteps = 0
		}

		// Camera: the one-shot fit latch (ADR-0224 §SD4).
		refit := v.fitRequested || (!v.everHadNodes && n > 0)
		v.fitRequested = false
		if n > 0 {
			v.everHadNodes = true
		}
		if refit {
			v.fitPending = true
			v.fitFrames = 0
		}
		doFit := v.Opts.FitToScreen
		if v.fitPending {
			v.fitFrames++
			if v.IsSettled() && v.fitFrames >= fitMinFrames {
				v.fitPending = false
			}
			doFit = true
		}
		if doFit {
			pad := v.Opts.FitPadding
			if pad <= 0 {
				pad = defaultFitPadding
			}
			if minX, minY, maxX, maxY, ok := v.g.bounds(v.style.NodeRadius); ok {
				v.cam.fit(minX, minY, maxX, maxY, w, h, pad)
			}
		}

		v.paint(w, h)

		// Interaction surfaces: the area region owns click and drag, the
		// canvas owns hover, containment and the wheel.
		c.PaintSenseRegion(v.ids.PrepareStr("graphview-area"), 0, 0, w, h).Send()
		cv := c.PaintCanvas(v.ids.PrepareStr("graphview-canvas"), w, h).
			Background(v.style.Background).
			Sense(true, true, true)
		if !v.Opts.NoZoomAndPan {
			cv = cv.CaptureZoom().CaptureScroll()
		}
		cv.Send()
	}
}

// pruneSelection drops selected nodes and edges the declaration no longer
// carries, silently: a vanished node has no position to report and a
// Deselect for it would name an id the caller just removed.
func (v *View) pruneSelection() {
	for id := range v.selNodes {
		if _, ok := v.g.slot[id]; !ok {
			delete(v.selNodes, id)
		}
	}
	for k := range v.selEdges {
		_, okF := v.g.slot[k[0]]
		_, okT := v.g.slot[k[1]]
		if !okF || !okT {
			delete(v.selEdges, k)
		}
	}
}

// applyInput turns last frame's registers into hover, selection, drag and
// camera changes, and queues the events they imply.
func (v *View) applyInput(w, h, px, py float32, posOk, inside bool,
	areaFlags c.ResponseFlagsE, wheel c.CanvasWheelValue) {
	o := &v.Opts
	// The pick under the pointer serves hover, click and drag alike.
	hitNode, hitEdge := int32(-1), int32(-1)
	if posOk && (inside || v.drag.active) {
		hitNode = v.pickNode(px, py)
		if hitNode < 0 {
			hitEdge = v.pickEdge(px, py)
		}
	}

	// Hover: the dragged node while a node drag is in flight, else the pick.
	// NoHover silences the state and its events either way; the dragged
	// node's highlight comes from the drag itself.
	hoverId, hoverOk := uint64(0), false
	switch {
	case o.NoHover:
	case v.drag.active && v.drag.isNode:
		hoverId, hoverOk = v.drag.nodeId, true
	case hitNode >= 0:
		hoverId, hoverOk = v.g.ids[hitNode], true
	}
	if hoverOk != v.hoveredOk || hoverId != v.hoveredId {
		if v.hoveredOk {
			v.events = append(v.events, v.nodeEventById(EventKindNodeHoverLeave, v.hoveredId))
		}
		if hoverOk {
			v.events = append(v.events, v.nodeEventById(EventKindNodeHoverEnter, hoverId))
		}
		v.hoveredId, v.hoveredOk = hoverId, hoverOk
	}
	v.hoveredEdge = -1
	if !o.NoHover && !v.drag.active {
		v.hoveredEdge = hitEdge
	}

	// Click. egui reports the second click of a double-click as a click
	// too; like the crate, the double-click takes it, so a double-clicked
	// node does not toggle its selection.
	if posOk && inside {
		switch {
		case areaFlags.HasDoubleClicked() && hitNode >= 0:
			if o.NodeClicking {
				v.events = append(v.events, v.nodeEvent(EventKindNodeDoubleClick, hitNode))
			}
		case areaFlags.HasPrimaryClicked() && hitNode >= 0:
			if o.NodeClicking {
				v.events = append(v.events, v.nodeEvent(EventKindNodeClick, hitNode))
			}
			if o.NodeSelection {
				v.toggleNodeSelection(v.g.ids[hitNode], o.NodeSelectionMulti)
			}
		case areaFlags.HasPrimaryClicked() && hitEdge >= 0:
			from, to := v.g.ids[v.g.eFrom[hitEdge]], v.g.ids[v.g.eTo[hitEdge]]
			if o.EdgeClicking {
				v.events = append(v.events, Event{Kind: EventKindEdgeClick, From: from, To: to})
			}
			if o.EdgeSelection {
				v.toggleEdgeSelection([2]uint64{from, to}, o.EdgeSelectionMulti)
			}
		case areaFlags.HasPrimaryClicked():
			v.deselectAll()
		}
	}

	// Drag: a node when the press lands on one, the camera otherwise. The
	// node is held by id, since slots move when the declaration shrinks.
	// Either kind releases the fit latch, so the view stops re-framing
	// under the gesture.
	if areaFlags.HasDragStarted() && posOk {
		v.drag = dragState{active: true, lastX: px, lastY: py}
		v.fitPending = false
		if hitNode >= 0 && !o.NoDragging {
			v.drag.isNode, v.drag.nodeId = true, v.g.ids[hitNode]
			v.events = append(v.events, v.nodeEvent(EventKindNodeDragStart, hitNode))
		}
	}
	// egui reports no drag on the release frame, but the pointer may have
	// moved since the last one; the stop frame's motion counts too.
	if v.drag.active && (areaFlags.HasDragged() || areaFlags.HasDragStopped()) && posOk {
		dx, dy := px-v.drag.lastX, py-v.drag.lastY
		v.drag.lastX, v.drag.lastY = px, py
		if v.drag.isNode {
			if s, ok := v.g.slot[v.drag.nodeId]; ok {
				v.g.x[s] += dx / v.cam.zoom
				v.g.y[s] += dy / v.cam.zoom
			}
		} else if !o.NoZoomAndPan {
			v.cam.panX += dx
			v.cam.panY += dy
		}
	}
	if v.drag.active && (areaFlags.HasDragStopped() || !areaFlags.HasIsPointerButtonDown()) {
		if v.drag.isNode {
			if s, ok := v.g.slot[v.drag.nodeId]; ok {
				if o.PinOnDrag {
					v.g.held[s] = true
				}
				v.events = append(v.events, v.nodeEvent(EventKindNodeDragEnd, s))
			}
		}
		v.drag = dragState{}
	}

	// Wheel: anchored zoom and scroll-pan, both scoped to this canvas.
	if !o.NoZoomAndPan {
		if z := wheel.Zoom; z > 0 && z != 1 {
			if o.ZoomSpeed > 0 && o.ZoomSpeed != 1 {
				z = float32(math.Pow(float64(z), float64(o.ZoomSpeed)))
			}
			ax, ay := zoomAnchor(wheel, px, py, posOk, w, h)
			v.cam.zoomAround(z, ax, ay)
			v.fitPending = false
		}
		if wheel.ScrollX != 0 || wheel.ScrollY != 0 {
			v.cam.panX += wheel.ScrollX
			v.cam.panY += wheel.ScrollY
			v.fitPending = false
		}
	}
}

// zoomAnchor picks the canvas point a wheel zoom keeps fixed: the wheel
// row's own hover when the canvas reported one, else the pointer the frame
// resolved, else the canvas centre. The row's hover is NaN whenever another
// widget is egui's topmost at the pointer — which the sense region over
// this canvas always is — so the fallbacks carry the common case.
func zoomAnchor(wheel c.CanvasWheelValue, px, py float32, posOk bool, w, h float32) (ax, ay float32) {
	switch {
	case !isNaN32(wheel.HoverX) && !isNaN32(wheel.HoverY):
		return wheel.HoverX, wheel.HoverY
	case posOk:
		return px, py
	}
	return w / 2, h / 2
}

func (v *View) toggleNodeSelection(id uint64, multi bool) {
	if _, sel := v.selNodes[id]; sel {
		delete(v.selNodes, id)
		v.events = append(v.events, v.nodeEventById(EventKindNodeDeselect, id))
		return
	}
	if !multi {
		v.deselectAll()
	}
	v.selNodes[id] = struct{}{}
	v.events = append(v.events, v.nodeEventById(EventKindNodeSelect, id))
}

// nodeEvent builds a node event carrying the slot's world position.
func (v *View) nodeEvent(kind EventKindE, slot int32) Event {
	return Event{Kind: kind, Node: v.g.ids[slot], X: v.g.x[slot], Y: v.g.y[slot]}
}

// nodeEventById is nodeEvent for an id that may no longer have a slot.
func (v *View) nodeEventById(kind EventKindE, id uint64) Event {
	if s, ok := v.g.slot[id]; ok {
		return v.nodeEvent(kind, s)
	}
	return Event{Kind: kind, Node: id}
}

// dragSlot is the slot of the node under the user's drag, or -1.
func (v *View) dragSlot() int32 {
	if v.drag.active && v.drag.isNode {
		if s, ok := v.g.slot[v.drag.nodeId]; ok {
			return s
		}
	}
	return -1
}

func (v *View) toggleEdgeSelection(k [2]uint64, multi bool) {
	if _, sel := v.selEdges[k]; sel {
		delete(v.selEdges, k)
		v.events = append(v.events, Event{Kind: EventKindEdgeDeselect, From: k[0], To: k[1]})
		return
	}
	if !multi {
		v.deselectAll()
	}
	v.selEdges[k] = struct{}{}
	v.events = append(v.events, Event{Kind: EventKindEdgeSelect, From: k[0], To: k[1]})
}

func (v *View) deselectAll() {
	for id := range v.SelectedNodes() {
		v.events = append(v.events, v.nodeEventById(EventKindNodeDeselect, id))
	}
	for k := range v.SelectedEdges() {
		v.events = append(v.events, Event{Kind: EventKindEdgeDeselect, From: k[0], To: k[1]})
	}
	clear(v.selNodes)
	clear(v.selEdges)
}

// pickMinPx is the smallest pick radius in screen pixels, so a node zoomed
// down to a dot stays clickable.
const pickMinPx = 6

// pickNode returns the slot under canvas point (px, py): the nearest node
// whose screen disc contains it, or -1. Linear in the node count
// (ADR-0224 §SD3).
func (v *View) pickNode(px, py float32) int32 {
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for i := range v.g.ids {
		sx, sy := v.cam.toScreen(v.g.x[i], v.g.y[i])
		r := max(v.nodeOuterPx(i), pickMinPx)
		dx, dy := px-sx, py-sy
		d2 := dx*dx + dy*dy
		if d2 <= r*r && d2 < bestD {
			best, bestD = int32(i), d2
		}
	}
	return best
}

// pickEdge returns the edge under canvas point (px, py) within a few pixels
// of its stroke, or -1. Curved edges are tested against a sampled polyline;
// self-loops against their loop circle.
func (v *View) pickEdge(px, py float32) int32 {
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for i := range v.g.eFrom {
		geo := v.edgeGeometry(i)
		tol := max(geo.width, 4)
		var d float32
		switch geo.kind {
		case edgeKindLoop:
			dist := float32(math.Hypot(float64(px-geo.loopCx), float64(py-geo.loopCy)))
			d = float32(math.Abs(float64(dist - geo.loopR)))
		case edgeKindStraight:
			d = distSegment(geo.x[0], geo.y[0], geo.x[3], geo.y[3], px, py)
		default:
			d = distBezier(geo.x, geo.y, px, py)
		}
		if d <= tol && d < bestD {
			best, bestD = int32(i), d
		}
	}
	return best
}

// nodeOuterPx is the node's radius on screen including its donut ring, the
// extent the pick, the highlight and the label respect.
func (v *View) nodeOuterPx(slot int) float32 {
	r := v.nodeRadius(slot) * v.cam.zoom
	if r >= donutMinInnerPx && !v.g.donut[slot].IsEmpty() {
		r += v.style.DonutWidth
	}
	return r
}

// nodeRadius is the node's world radius: its own, else the style's.
func (v *View) nodeRadius(slot int) float32 {
	if r := v.g.radius[slot]; r > 0 {
		return r
	}
	return v.style.NodeRadius
}

// nodeFill is the node's fill: its own literal colour, else the style's. A
// retained colour has no literal to batch on and takes the style's too.
func (v *View) nodeFill(slot int) color.Color {
	if col := v.g.col[slot]; col.Kind() == color.ColorKindLiteral && col.Literal() != 0 {
		return col
	}
	return v.style.NodeFill
}

func isNaN32(f float32) bool { return f != f }
