package graphview

import (
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

	ids *c.WidgetIdStack
	key string

	g   graph
	fs  forceState
	cam camera

	hierDone     bool
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
	lastW    float32
	lastH    float32
	originX  float32 // canvas top-left in screen pixels, from the R24 row
	originY  float32
	originOk bool

	// scratch
	batches  map[uint64][]int32
	batchXs  []float32
	batchYs  []float32
	selOrder []uint64
	arcs     []donutArc
	arcXs    []float32
	arcYs    []float32
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
		cam:         camera{zoom: 1},
		hoveredEdge: -1,
		selNodes:    make(map[uint64]struct{}, 8),
		selEdges:    make(map[[2]uint64]struct{}, 8),
		batches:     make(map[uint64][]int32, 8),
		fs:          forceState{lastDisp: nan32},
	}
}

// FitNow re-arms the one-shot fit: the camera frames the graph while the
// layout settles, then latches off again. Fire once, not every frame.
func (v *View) FitNow() { v.fitRequested = true }

// ResetLayout discards every position before the next render: nodes are
// placed afresh by the layout, the step counter restarts and the camera
// re-fits.
func (v *View) ResetLayout() { v.resetPending = true }

// FastForward advances the force simulation by steps extra iterations
// before the next render. A no-op for the static layouts.
func (v *View) FastForward(steps uint32) { v.ffSteps += steps }

// Events returns the interactions the previous frame's input produced,
// valid until the next Render.
func (v *View) Events() []Event { return v.events }

// Metrics returns node and edge counts and the force layout's step counter
// and last average displacement.
func (v *View) Metrics() Metrics {
	return Metrics{
		NodeCount:        uint32(v.g.n()),
		EdgeCount:        uint32(len(v.g.eFrom)),
		Steps:            v.fs.steps,
		LastDisplacement: v.fs.lastDisp,
	}
}

// IsSettled reports whether the layout has stopped moving: the force
// layouts once their average displacement is at or under Epsilon or they are
// paused, the static layouts always.
func (v *View) IsSettled() bool {
	if !v.Opts.Layout.IsAnimated() {
		return true
	}
	if v.Opts.Force.Paused {
		return true
	}
	return v.fs.settled(v.Opts.Force.withDefaults().Epsilon)
}

// Hovered returns the node under the pointer, as of the previous frame.
func (v *View) Hovered() (id uint64, ok bool) {
	return v.hoveredId, v.hoveredOk
}

// SelectedNodes yields the selected node ids in ascending order.
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

// SelectedEdges yields the selected edges as (from, to) pairs, ordered.
func (v *View) SelectedEdges() iter.Seq[[2]uint64] {
	return func(yield func([2]uint64) bool) {
		keys := make([][2]uint64, 0, len(v.selEdges))
		for k := range v.selEdges {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b [2]uint64) int {
			if r := cmpU64(a[0], b[0]); r != 0 {
				return r
			}
			return cmpU64(a[1], b[1])
		})
		for _, k := range keys {
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

// NodeScreenPosition returns where a node was painted in the last Render,
// in canvas pixels from the canvas's top-left — the anchor for a tooltip
// or an overlay drawn beside the node.
func (v *View) NodeScreenPosition(id uint64) (x, y float32, ok bool) {
	s, ok := v.g.slot[id]
	if !ok {
		return
	}
	x, y = v.cam.toScreen(v.g.x[s], v.g.y[s])
	return x, y, true
}

// CanvasScreenOrigin returns the canvas's top-left in screen pixels as the
// host reported it for the previous frame — the offset that turns a
// NodeScreenPosition into a screen point for an overlay outside the canvas.
// ok is false until the canvas has rendered once.
func (v *View) CanvasScreenOrigin() (x, y float32, ok bool) {
	return v.originX, v.originY, v.originOk
}

// SetNodePosition moves a node in world units; a force layout continues
// from there.
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
	if pw, ph, ok := c.CapturePaneSize(v.ids.PrepareStr(v.key + "-pane").Derive()); ok && pw > 0 && ph > 0 {
		w, h = pw, ph
	}
	v.Render(nodes, edges, w, h)
}

// Render reconciles the declaration, applies the previous frame's input,
// advances the layout and paints into a w×h canvas at the current layout
// position.
func (v *View) Render(nodes []NodeSpec, edges []EdgeSpec, w, h float32) {
	v.events = v.events[:0]
	if w <= 0 || h <= 0 {
		return
	}
	v.lastW, v.lastH = w, h
	style := v.Opts.Style.withDefaults()
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
		n := v.g.n()

		if v.resetPending {
			v.resetPending = false
			v.hierDone = false
			v.fs.reset()
			for i := range v.g.pinned {
				v.g.pinned[i] = false
			}
			created = allSlots(n)
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
		if v.Opts.Layout == LayoutHierarchical && (topoChanged || !v.hierDone || len(created) > 0) {
			layoutHierarchical(&v.g, hp)
			v.hierDone = true
		}

		// Input, against the previous frame's geometry.
		v.applyInput(style, px, py, posOk, inside, areaFlags, wheel)

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
				pad = 0.1
			}
			if minX, minY, maxX, maxY, ok := v.g.bounds(style.NodeRadius); ok {
				v.cam.fit(minX, minY, maxX, maxY, w, h, pad)
			}
		}

		v.paint(style, w, h)

		// Interaction surfaces: the area region owns click and drag, the
		// canvas owns hover, containment and the wheel.
		c.PaintSenseRegion(v.ids.PrepareStr("graphview-area"), 0, 0, w, h).Send()
		cv := c.PaintCanvas(v.ids.PrepareStr("graphview-canvas"), w, h).
			Background(style.Background).
			Sense(true, true, true)
		if !v.Opts.NoZoomAndPan {
			cv = cv.CaptureZoom().CaptureScroll()
		}
		cv.Send()
	}
}

func allSlots(n int) []int32 {
	s := make([]int32, n)
	for i := range s {
		s[i] = int32(i)
	}
	return s
}

// applyInput turns last frame's registers into hover, selection, drag and
// camera changes, and queues the events they imply.
func (v *View) applyInput(style Style, px, py float32, posOk, inside bool,
	areaFlags c.ResponseFlagsE, wheel c.CanvasWheelValue) {
	o := &v.Opts
	// The pick under the pointer serves hover, click and drag alike; only
	// the hover state and its events are gated by NoHover.
	hitNode, hitEdge := int32(-1), int32(-1)
	if posOk && (inside || v.drag.active) {
		hitNode = v.pickNode(style, px, py)
		if hitNode < 0 {
			hitEdge = v.pickEdge(style, px, py)
		}
	}
	hoverId, hoverOk := uint64(0), false
	switch {
	case v.drag.active && v.drag.isNode:
		hoverId, hoverOk = v.drag.nodeId, true
	case hitNode >= 0 && !o.NoHover:
		hoverId, hoverOk = v.g.ids[hitNode], true
	}
	if hoverOk != v.hoveredOk || hoverId != v.hoveredId {
		if v.hoveredOk {
			v.events = append(v.events, Event{Kind: EventKindNodeHoverLeave, Node: v.hoveredId})
		}
		if hoverOk {
			v.events = append(v.events, Event{Kind: EventKindNodeHoverEnter, Node: hoverId})
		}
		v.hoveredId, v.hoveredOk = hoverId, hoverOk
	}
	v.hoveredEdge = -1
	if !o.NoHover && !v.drag.active {
		v.hoveredEdge = hitEdge
	}

	// Click and double-click.
	if posOk && inside && areaFlags.HasPrimaryClicked() {
		switch {
		case hitNode >= 0:
			id := v.g.ids[hitNode]
			if o.NodeClicking {
				v.events = append(v.events, Event{Kind: EventKindNodeClick, Node: id})
			}
			if o.NodeSelection {
				v.toggleNodeSelection(id, o.NodeSelectionMulti)
			}
		case hitEdge >= 0:
			from, to := v.g.ids[v.g.eFrom[hitEdge]], v.g.ids[v.g.eTo[hitEdge]]
			if o.EdgeClicking {
				v.events = append(v.events, Event{Kind: EventKindEdgeClick, From: from, To: to})
			}
			if o.EdgeSelection {
				v.toggleEdgeSelection([2]uint64{from, to}, o.EdgeSelectionMulti)
			}
		default:
			v.deselectAll()
		}
	}
	if posOk && inside && areaFlags.HasDoubleClicked() && hitNode >= 0 && o.NodeClicking {
		v.events = append(v.events, Event{Kind: EventKindNodeDoubleClick, Node: v.g.ids[hitNode]})
	}

	// Drag: a node when the press lands on one, the camera otherwise. The
	// node is held by id, since slots move when the declaration shrinks.
	if areaFlags.HasDragStarted() && posOk {
		v.drag = dragState{active: true, lastX: px, lastY: py}
		if hitNode >= 0 && !o.NoDragging {
			id := v.g.ids[hitNode]
			v.drag.isNode, v.drag.nodeId = true, id
			v.g.pinned[hitNode] = true
			v.events = append(v.events, Event{Kind: EventKindNodeDragStart, Node: id})
		}
	}
	if v.drag.active && areaFlags.HasDragged() && posOk {
		dx, dy := px-v.drag.lastX, py-v.drag.lastY
		v.drag.lastX, v.drag.lastY = px, py
		if v.drag.isNode {
			if s, ok := v.g.slot[v.drag.nodeId]; ok {
				v.g.pinned[s] = true
				v.g.x[s] += dx / v.cam.zoom
				v.g.y[s] += dy / v.cam.zoom
			}
		} else if !o.NoZoomAndPan {
			v.cam.panX += dx
			v.cam.panY += dy
			v.fitPending = false
		}
	}
	if v.drag.active && (areaFlags.HasDragStopped() || !areaFlags.HasIsPointerButtonDown()) {
		if v.drag.isNode {
			if s, ok := v.g.slot[v.drag.nodeId]; ok {
				v.g.pinned[s] = false
			}
			v.events = append(v.events, Event{Kind: EventKindNodeDragEnd, Node: v.drag.nodeId})
		}
		v.drag = dragState{}
	}

	// Wheel: anchored zoom and scroll-pan, both scoped to this canvas.
	if !o.NoZoomAndPan {
		if z := wheel.Zoom; z > 0 && z != 1 {
			ax, ay := zoomAnchor(wheel, px, py, posOk, v.lastW, v.lastH)
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
		v.events = append(v.events, Event{Kind: EventKindNodeDeselect, Node: id})
		return
	}
	if !multi {
		v.deselectAll()
	}
	v.selNodes[id] = struct{}{}
	v.events = append(v.events, Event{Kind: EventKindNodeSelect, Node: id})
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
		v.events = append(v.events, Event{Kind: EventKindNodeDeselect, Node: id})
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
func (v *View) pickNode(style Style, px, py float32) int32 {
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for i := range v.g.ids {
		sx, sy := v.cam.toScreen(v.g.x[i], v.g.y[i])
		r := v.nodeOuterPx(style, i)
		r = max(r, pickMinPx)
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
func (v *View) pickEdge(style Style, px, py float32) int32 {
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for i := range v.g.eFrom {
		geo := v.edgeGeometry(style, i)
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
func (v *View) nodeOuterPx(style Style, slot int) float32 {
	r := v.nodeRadius(style, slot) * v.cam.zoom
	if r >= donutMinInnerPx && !v.g.donut[slot].IsEmpty() {
		r += style.DonutWidth
	}
	return r
}

func (v *View) nodeRadius(style Style, slot int) float32 {
	if r := v.g.radius[slot]; r > 0 {
		return r
	}
	return style.NodeRadius
}

func isNaN32(f float32) bool { return f != f }

// color batching key: literal rgba in the high word, radius bits below.
func batchKey(col color.Color, radius float32) uint64 {
	return uint64(col.Literal())<<32 | uint64(math.Float32bits(radius))
}
