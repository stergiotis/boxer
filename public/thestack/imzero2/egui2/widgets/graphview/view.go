package graphview

import (
	"cmp"
	"iter"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/legend"
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
	hoveredEdge int32   // edge index in this frame's edge arrays, or -1
	hovEdge     EdgeRef // the hovered edge as last reported, for enter/leave
	hovEdgeOk   bool
	selNodes    map[uint64]struct{}
	selEdges    map[EdgeRef]struct{}
	drag        dragState

	lastW, lastH float32  // canvas size of the last Render, 0 before it
	fitIds       []uint64 // FitNodes request, applied at once or on the next Render
	fitIdsWait   bool
	autoPaused   bool // ForceParams.PauseOnSettle hold
	lastForce    ForceParams
	camMoved     bool

	events   []Event
	originX  float32 // canvas top-left in screen pixels, from the R24 row
	originY  float32
	originOk bool

	// Auras (ADR-0224 §SD11): the frame's aura table, the field, one ring
	// set per aura and the paint order; the rings are reused while the
	// cache key holds and nothing moved.
	auraSet     auraSet
	auraF       auraField
	auraRings   []rings
	auraOrder   []int32
	hiddenAuras map[string]struct{}
	hiddenVer   uint32
	auraDirty   bool         // a node moved by other means than the force step
	auraDrift   float32      // force-step displacement since the last field, world units
	auraKey     auraCacheKey // what the rings were computed for; zero before the first
	legendItems []legend.Item

	// scratch
	batchIdx     map[uint64]int32
	batches      []nodeBatch
	batchXs      []float32
	batchYs      []float32
	tipXs        [3]float32
	tipYs        [3]float32
	selOrder     []uint64
	selEdgeOrder []EdgeRef
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
	isNode bool    // moving a node rather than panning
	isRect bool    // sweeping a selection rectangle rather than panning
	nodeId uint64  // the node, when isNode
	x0, y0 float32 // press position in canvas pixels, the rectangle's anchor
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
		style:       opts.Style.withDefaults(),
		hoveredEdge: -1,
		selNodes:    make(map[uint64]struct{}, 8),
		selEdges:    make(map[EdgeRef]struct{}, 8),
		hiddenAuras: make(map[string]struct{}, 4),
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

// FitNodes frames the given nodes, padded by Options.FitPadding, and
// releases any pending fit so the framing sticks — the fit-to-subset a
// "focus" action wants. Unknown ids are skipped; with none known the camera
// is left alone. Before the first Render the request waits for it, since the
// canvas size is not known yet.
func (v *View) FitNodes(ids []uint64) {
	v.fitIds = append(v.fitIds[:0], ids...)
	v.fitIdsWait = true
	if v.lastW > 0 && v.lastH > 0 {
		v.applyFitNodes(v.lastW, v.lastH)
	}
}

func (v *View) applyFitNodes(w, h float32) {
	v.fitIdsWait = false
	var minX, minY, maxX, maxY float32
	found := false
	for _, id := range v.fitIds {
		s, ok := v.g.slot[id]
		if !ok {
			continue
		}
		r := v.nodeRadius(int(s))
		x, y := v.g.x[s], v.g.y[s]
		if !found {
			minX, minY, maxX, maxY = x-r, y-r, x+r, y+r
			found = true
			continue
		}
		minX = min(minX, x-r)
		minY = min(minY, y-r)
		maxX = max(maxX, x+r)
		maxY = max(maxY, y+r)
	}
	if !found {
		return
	}
	v.cam.fit(minX, minY, maxX, maxY, w, h, v.fitPadding())
	v.fitPending = false
}

// fitPadding is Options.FitPadding with its default.
func (v *View) fitPadding() float32 {
	if p := v.Opts.FitPadding; p > 0 {
		return p
	}
	return defaultFitPadding
}

// AuraIds yields the aura ids of the last render's declaration in id
// order, hidden ones included — the rows a legend lists.
func (v *View) AuraIds() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, id := range v.auraSet.ids {
			if !yield(id) {
				return
			}
		}
	}
}

// HideAura stops drawing the aura and its members' contribution to it; the
// nodes stay. ShowAura reverses it. Neither reports an event — the caller
// asked.
func (v *View) HideAura(id string) { v.setAuraHidden(id, true) }

// ShowAura undoes HideAura.
func (v *View) ShowAura(id string) { v.setAuraHidden(id, false) }

// AuraHidden reports whether the aura is hidden, by HideAura or a legend
// click.
func (v *View) AuraHidden(id string) bool {
	_, hidden := v.hiddenAuras[id]
	return hidden
}

func (v *View) setAuraHidden(id string, hidden bool) {
	if _, was := v.hiddenAuras[id]; was == hidden {
		return
	}
	if hidden {
		v.hiddenAuras[id] = struct{}{}
	} else {
		delete(v.hiddenAuras, id)
	}
	v.hiddenVer++
}

// FastForward advances the force simulation by steps extra iterations
// before the next render. A no-op for the static layouts.
func (v *View) FastForward(steps uint32) {
	v.ffSteps += steps
	v.autoPaused = false
}

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
		Paused:           v.Opts.Layout.IsAnimated() && (v.Opts.Force.Paused || v.autoPaused),
		CameraMoved:      v.camMoved,
	}
}

// IsSettled reports whether the layout has stopped moving for any reason —
// the predicate the fit latch waits on: a static layout always, a paused
// force layout (by Paused or the PauseOnSettle hold), or a force layout
// whose average displacement is at or under Epsilon. Metrics.Settled is the
// convergence test alone.
func (v *View) IsSettled() bool {
	if !v.Opts.Layout.IsAnimated() || v.Opts.Force.Paused || v.autoPaused {
		return true
	}
	return v.fs.settled(v.Opts.Force.withDefaults().Epsilon)
}

// HoveredNode returns the node under the pointer, as of the previous frame.
func (v *View) HoveredNode() (id uint64, ok bool) {
	return v.hoveredId, v.hoveredOk
}

// HoveredEdge returns the edge under the pointer, as of the previous frame,
// when no node is. Parallel edges of one ordered pair are told apart only
// by their EdgeSpec.Id.
func (v *View) HoveredEdge() (ref EdgeRef, ok bool) {
	if e := v.hoveredEdge; e >= 0 && int(e) < len(v.g.eFrom) {
		return v.g.edgeRef(e), true
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

// SelectedEdges yields the selected edges ordered by (from, to, id). The
// same scratch caveat as SelectedNodes applies.
func (v *View) SelectedEdges() iter.Seq[EdgeRef] {
	return func(yield func(EdgeRef) bool) {
		v.selEdgeOrder = v.selEdgeOrder[:0]
		for k := range v.selEdges {
			v.selEdgeOrder = append(v.selEdgeOrder, k)
		}
		slices.SortFunc(v.selEdgeOrder, func(a, b EdgeRef) int {
			if r := cmp.Compare(a.From, b.From); r != 0 {
				return r
			}
			if r := cmp.Compare(a.To, b.To); r != 0 {
				return r
			}
			return cmp.Compare(a.Id, b.Id)
		})
		for _, k := range v.selEdgeOrder {
			if !yield(k) {
				return
			}
		}
	}
}

// IsNodeSelected reports whether the node is selected.
func (v *View) IsNodeSelected(id uint64) bool {
	_, sel := v.selNodes[id]
	return sel
}

// IsEdgeSelected reports whether the edge is selected.
func (v *View) IsEdgeSelected(ref EdgeRef) bool {
	_, sel := v.selEdges[ref]
	return sel
}

// SelectNode adds a node to the selection from code (ADR-0224 §SD12). Like
// HideAura it reports no event — the caller asked — and it does not enforce
// single selection: ClearSelection first for that. It reports false, and
// does nothing, for an id the declaration does not carry.
func (v *View) SelectNode(id uint64) bool {
	if _, ok := v.g.slot[id]; !ok {
		return false
	}
	v.selNodes[id] = struct{}{}
	return true
}

// DeselectNode removes a node from the selection, silently.
func (v *View) DeselectNode(id uint64) { delete(v.selNodes, id) }

// SelectEdge adds an edge to the selection from code, with SelectNode's
// rules. The ref must name an edge of the last declaration, id included.
func (v *View) SelectEdge(ref EdgeRef) bool {
	if v.g.findEdge(ref) < 0 {
		return false
	}
	v.selEdges[ref] = struct{}{}
	return true
}

// DeselectEdge removes an edge from the selection, silently.
func (v *View) DeselectEdge(ref EdgeRef) { delete(v.selEdges, ref) }

// ClearSelection empties the node and edge selection, silently.
func (v *View) ClearSelection() {
	clear(v.selNodes)
	clear(v.selEdges)
}

// Positions yields every declared node's id and world position, in the
// widget's slot order, which is stable while the declaration is. It is the
// bulk read a caller saves a layout with; SetNodePosition restores it.
func (v *View) Positions() iter.Seq2[uint64, [2]float32] {
	return func(yield func(uint64, [2]float32) bool) {
		for i, id := range v.g.ids {
			if !yield(id, [2]float32{v.g.x[i], v.g.y[i]}) {
				return
			}
		}
	}
}

// NodeScreenRadius returns the node's radius as painted in the last Render,
// in canvas pixels, donut ring included — the extent an overlay placed by
// NodeCanvasPosition should clear.
func (v *View) NodeScreenRadius(id uint64) (r float32, ok bool) {
	s, ok := v.g.slot[id]
	if !ok {
		return
	}
	return v.nodeOuterPx(int(s)), true
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
	v.cam.zoom = v.cam.clamp(zoom)
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
		v.auraDirty = true
		v.autoPaused = false
	}
}

// UnpinNode releases a widget-side pin set by PinNode or Options.PinOnDrag.
// A pin declared on the NodeSpec is the caller's to drop.
func (v *View) UnpinNode(id uint64) {
	if s, ok := v.g.slot[id]; ok {
		v.g.held[s] = false
		v.autoPaused = false
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
		v.auraDirty = true
		v.autoPaused = false
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
	v.camMoved = false
	if w <= 0 || h <= 0 {
		return
	}
	v.lastW, v.lastH = w, h
	v.style = v.Opts.Style.withDefaults()
	fp := v.Opts.Force.withDefaults()
	hp := v.Opts.Hier.withDefaults()
	ap := v.Opts.Auras.withDefaults()
	v.cam.setLimits(v.Opts.ZoomMin, v.Opts.ZoomMax)
	camBefore := v.cam

	for range c.IdScope(v.ids.PrepareStr(v.key)) {
		sm := c.CurrentApplicationState.StateManager
		canvasH := widgethandle.Make(v.ids.PrepareStr("graphview-canvas").Derive())
		areaH := widgethandle.Make(v.ids.PrepareStr("graphview-area").Derive())
		canvasFlags := sm.GetResponse(canvasH)
		areaFlags := sm.GetResponse(areaH)
		wheel := sm.GetCanvasWheel(canvasH)
		mods := sm.GetModifiers()

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

		// The aura legend's rows of the previous frame: their regions sit
		// above the area region, so a row's click is not also a canvas
		// click, and the toggle lands in this frame's field.
		if ap.Enabled && ap.Legend && len(v.legendItems) > 0 {
			if clicked, _ := legend.Read(sm, v.ids, auraLegendPrefix, v.legendItems); clicked >= 0 {
				id := v.legendItems[clicked].Key
				v.setAuraHidden(id, !v.AuraHidden(id))
				v.events = append(v.events, Event{Kind: EventKindAuraToggle, Aura: id})
			}
		}

		created, topoChanged := v.g.reconcile(nodes, edges)
		if topoChanged {
			v.pruneSelection()
		}
		n := v.g.n()
		// The table is kept current even with auras off, so AuraIds answers.
		if v.auraSet.build(nodes, &v.g) {
			v.auraDirty = true
		}

		if v.resetPending {
			v.resetPending = false
			v.hierDone = false
			v.fs.reset()
			clear(v.g.held)
			created = v.g.allSlots()
			v.drag = dragState{}
			v.fitRequested = true
			v.auraDirty = true
			v.autoPaused = false
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
			v.auraDirty = true
		}
		// Declared pins win over any placement; the drag in flight keeps its
		// node where the pointer has it (ADR-0224 §SD10).
		if v.g.applyPins(v.dragSlot()) {
			v.auraDirty = true
			v.autoPaused = false
		}

		// Input, against the previous frame's geometry. A drag that began
		// this frame fixes its node before the step below can move it.
		v.applyInput(w, h, px, py, posOk, inside, areaFlags, wheel, mods)
		if s := v.dragSlot(); s >= 0 {
			v.g.fixed[s] = true
		}

		// Layout.
		v.stepLayout(w, h, fp, topoChanged || len(created) > 0)

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
			pad := v.fitPadding()
			if minX, minY, maxX, maxY, ok := v.g.bounds(v.style.NodeRadius); ok {
				v.cam.fit(minX, minY, maxX, maxY, w, h, pad)
				// Auras reach past the nodes by a screen-space amount; widen
				// the box by it, in world units at the zoom just fitted, and
				// fit again so the blobs stay in frame.
				if m := v.auraFitMargin(ap); m > 0 {
					v.cam.fit(minX-m, minY-m, maxX+m, maxY+m, w, h, pad)
				}
			}
		}

		if v.fitIdsWait {
			v.applyFitNodes(w, h)
		}
		v.camMoved = !v.cam.same(camBefore)

		v.updateAuras(ap, w, h)
		v.paint(w, h)

		// Interaction surfaces: the area region owns click and drag, the
		// canvas owns hover, containment and the wheel; the legend's rows
		// come after the area so they win its clicks.
		c.PaintSenseRegion(v.ids.PrepareStr("graphview-area"), 0, 0, w, h).Send()
		v.emitAuraLegend(ap)
		cv := c.PaintCanvas(v.ids.PrepareStr("graphview-canvas"), w, h).
			Background(v.style.Background).
			Sense(true, true, true)
		if !v.Opts.NoZoomAndPan {
			cv = cv.CaptureZoom().CaptureScroll()
		}
		cv.Send()
	}
}

// stepLayout advances a force layout by this frame's steps: the fast-forward
// backlog plus one, unless paused by ForceParams.Paused or by the
// PauseOnSettle hold. wake is a topology change; a drag, FastForward, a
// parameter change or the setters lift the hold too, and it is taken once
// the step reports settled.
func (v *View) stepLayout(w, h float32, fp ForceParams, wake bool) {
	if !v.Opts.Layout.IsAnimated() {
		v.ffSteps = 0
		return
	}
	if fp != v.lastForce {
		v.lastForce = fp
		v.autoPaused = false
	}
	if wake || v.dragSlot() >= 0 {
		v.autoPaused = false
	}
	cg := float32(0)
	if v.Opts.Layout == LayoutForceDirectedCG {
		cg = fp.CenterGravity
	}
	steps := v.ffSteps
	v.ffSteps = 0
	if !fp.Paused && !v.autoPaused {
		steps++
	}
	for range steps {
		v.fs.step(&v.g, w, h, fp, cg)
	}
	// The field follows the simulation once the nodes have drifted a
	// fraction of a cell: a settled layout still steps, by less than
	// epsilon, and this keeps that from either forcing a field per
	// frame or accumulating unseen.
	if steps > 0 && !isNaN32(v.fs.lastDisp) {
		v.auraDrift += v.fs.lastDisp * float32(steps)
	}
	if fp.PauseOnSettle && steps > 0 && v.fs.settled(fp.Epsilon) {
		v.autoPaused = true
	}
}

// pruneSelection drops selected nodes and edges the declaration no longer
// carries, silently: a vanished node has no position to report and a
// Deselect for it would name an id the caller just removed. An edge whose
// endpoints survive but which itself vanished is dropped too.
func (v *View) pruneSelection() {
	for id := range v.selNodes {
		if _, ok := v.g.slot[id]; !ok {
			delete(v.selNodes, id)
		}
	}
	for k := range v.selEdges {
		if v.g.findEdge(k) < 0 {
			delete(v.selEdges, k)
		}
	}
}

// applyInput turns last frame's registers into hover, selection, drag and
// camera changes, and queues the events they imply.
func (v *View) applyInput(w, h, px, py float32, posOk, inside bool,
	areaFlags c.ResponseFlagsE, wheel c.CanvasWheelValue, mods c.ModifiersValue) {
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
	edgeOk := v.hoveredEdge >= 0
	var edgeRef EdgeRef
	if edgeOk {
		edgeRef = v.g.edgeRef(v.hoveredEdge)
	}
	if edgeOk != v.hovEdgeOk || edgeRef != v.hovEdge {
		if v.hovEdgeOk {
			v.events = append(v.events, edgeEvent(EventKindEdgeHoverLeave, v.hovEdge))
		}
		if edgeOk {
			v.events = append(v.events, edgeEvent(EventKindEdgeHoverEnter, edgeRef))
		}
		v.hovEdge, v.hovEdgeOk = edgeRef, edgeOk
	}

	// Click. egui reports the second click of a double-click as a click
	// too; like the crate, the double-click takes it, so a double-clicked
	// node does not toggle its selection. A secondary click — the right
	// button, or a long touch — reports beside either and changes nothing.
	if posOk && inside {
		dbl := areaFlags.HasDoubleClicked()
		prim := areaFlags.HasPrimaryClicked()
		sec := areaFlags.HasSecondaryClicked() || areaFlags.HasLongTouched()
		switch {
		case hitNode >= 0:
			switch {
			case dbl:
				if o.NodeClicking {
					v.events = append(v.events, v.nodeEvent(EventKindNodeDoubleClick, hitNode))
				}
			case prim:
				if o.NodeClicking {
					v.events = append(v.events, v.nodeEvent(EventKindNodeClick, hitNode))
				}
				if o.NodeSelection {
					v.toggleNodeSelection(v.g.ids[hitNode], o.NodeSelectionMulti)
				}
			}
			if sec && o.NodeClicking {
				v.events = append(v.events, v.nodeEvent(EventKindNodeSecondaryClick, hitNode))
			}
		case hitEdge >= 0:
			ref := v.g.edgeRef(hitEdge)
			switch {
			case dbl:
				if o.EdgeClicking {
					v.events = append(v.events, edgeEvent(EventKindEdgeDoubleClick, ref))
				}
			case prim:
				if o.EdgeClicking {
					v.events = append(v.events, edgeEvent(EventKindEdgeClick, ref))
				}
				if o.EdgeSelection {
					v.toggleEdgeSelection(ref, o.EdgeSelectionMulti)
				}
			}
			if sec && o.EdgeClicking {
				v.events = append(v.events, edgeEvent(EventKindEdgeSecondaryClick, ref))
			}
		default:
			wx, wy := v.cam.toWorld(px, py)
			bg := func(kind EventKindE) {
				if o.BackgroundClicking {
					v.events = append(v.events, Event{Kind: kind, X: wx, Y: wy})
				}
			}
			switch {
			case dbl:
				bg(EventKindBackgroundDoubleClick)
			case prim:
				v.deselectAll()
				bg(EventKindBackgroundClick)
			}
			if sec {
				bg(EventKindBackgroundSecondaryClick)
			}
		}
	}

	// Drag: a node when the press lands on one, a selection rectangle on a
	// Shift-press over the background, the camera otherwise. The node is
	// held by id, since slots move when the declaration shrinks. Every kind
	// releases the fit latch, so the view stops re-framing under the
	// gesture.
	if areaFlags.HasDragStarted() && posOk {
		v.drag = dragState{active: true, x0: px, y0: py, lastX: px, lastY: py}
		v.fitPending = false
		switch {
		case hitNode >= 0 && !o.NoDragging:
			v.drag.isNode, v.drag.nodeId = true, v.g.ids[hitNode]
			v.events = append(v.events, v.nodeEvent(EventKindNodeDragStart, hitNode))
		case o.RectSelection && o.NodeSelection && mods.Shift:
			v.drag.isRect = true
		}
	}
	// egui reports no drag on the release frame, but the pointer may have
	// moved since the last one; the stop frame's motion counts too.
	if v.drag.active && (areaFlags.HasDragged() || areaFlags.HasDragStopped()) && posOk {
		dx, dy := px-v.drag.lastX, py-v.drag.lastY
		v.drag.lastX, v.drag.lastY = px, py
		switch {
		case v.drag.isNode:
			if s, ok := v.g.slot[v.drag.nodeId]; ok {
				v.g.x[s] += dx / v.cam.zoom
				v.g.y[s] += dy / v.cam.zoom
				v.auraDirty = true
			}
		case v.drag.isRect:
			// The rectangle is (x0, y0)–(lastX, lastY); nothing else moves.
		case !o.NoZoomAndPan:
			v.cam.panX += dx
			v.cam.panY += dy
		}
	}
	if v.drag.active && (areaFlags.HasDragStopped() || !areaFlags.HasIsPointerButtonDown()) {
		switch {
		case v.drag.isNode:
			if s, ok := v.g.slot[v.drag.nodeId]; ok {
				if o.PinOnDrag {
					v.g.held[s] = true
				}
				v.events = append(v.events, v.nodeEvent(EventKindNodeDragEnd, s))
			}
		case v.drag.isRect:
			v.rectSelect(v.drag.x0, v.drag.y0, v.drag.lastX, v.drag.lastY, o.NodeSelectionMulti)
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

func (v *View) toggleEdgeSelection(k EdgeRef, multi bool) {
	if _, sel := v.selEdges[k]; sel {
		delete(v.selEdges, k)
		v.events = append(v.events, edgeEvent(EventKindEdgeDeselect, k))
		return
	}
	if !multi {
		v.deselectAll()
	}
	v.selEdges[k] = struct{}{}
	v.events = append(v.events, edgeEvent(EventKindEdgeSelect, k))
}

// edgeEvent builds an edge event from a ref.
func edgeEvent(kind EventKindE, k EdgeRef) Event {
	return Event{Kind: kind, From: k.From, To: k.To, Edge: k.Id}
}

func (v *View) deselectAll() {
	for id := range v.SelectedNodes() {
		v.events = append(v.events, v.nodeEventById(EventKindNodeDeselect, id))
	}
	for k := range v.SelectedEdges() {
		v.events = append(v.events, edgeEvent(EventKindEdgeDeselect, k))
	}
	clear(v.selNodes)
	clear(v.selEdges)
}

// rectSelect selects the nodes whose centres the canvas rectangle covers,
// reporting a Select per node, after a Deselect for the previous selection
// unless add is set.
func (v *View) rectSelect(x0, y0, x1, y1 float32, add bool) {
	minX, maxX := min(x0, x1), max(x0, x1)
	minY, maxY := min(y0, y1), max(y0, y1)
	if !add {
		v.deselectAll()
	}
	for i := range v.g.ids {
		sx, sy := v.cam.toScreen(v.g.x[i], v.g.y[i])
		if sx < minX || sx > maxX || sy < minY || sy > maxY {
			continue
		}
		id := v.g.ids[i]
		if _, sel := v.selNodes[id]; sel {
			continue
		}
		v.selNodes[id] = struct{}{}
		v.events = append(v.events, v.nodeEvent(EventKindNodeSelect, int32(i)))
	}
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

// auraLegendPrefix keys the aura legend's row regions under the view's ids.
const auraLegendPrefix = "graphview-aura-legend-"

// auraLegendInset is the legend box's offset from the canvas's top-left.
const auraLegendInset = 8

// auraCacheKey is everything besides node positions that the aura rings
// depend on; a change recomputes them.
type auraCacheKey struct {
	cam       camera
	w, h      float32
	params    auraParamsKey
	hash      uint64
	hiddenVer uint32
}

// auraFitMargin is how far, in world units, the widest aura reaches past
// its node's disc at the current zoom, or 0 without auras.
func (v *View) auraFitMargin(ap AuraParams) float32 {
	if !ap.Enabled || len(v.auraSet.ids) == 0 {
		return 0
	}
	maxR := float32(0)
	for s := range v.g.ids {
		if len(v.auraSet.members(int32(s))) == 0 {
			continue
		}
		maxR = max(maxR, v.nodeRadius(s))
	}
	if maxR <= 0 {
		return 0
	}
	ext := kernelFor(maxR*v.cam.zoom, ap).extent(ap.DrawLimit) / v.cam.zoom
	return max(ext-maxR, 0)
}

// auraDriftCells is the force-step drift, in cells on screen, past which
// the rings are recomputed.
const auraDriftCells = 0.25

// updateAuras recomputes the field and the rings when anything they depend
// on changed: the camera, the canvas, the parameters, the membership, the
// hidden set, or a node position — the setters' dirty flag or enough
// simulation drift. The zero key never matches a real one, since w and h
// are positive whenever this runs.
func (v *View) updateAuras(ap AuraParams, w, h float32) {
	na := len(v.auraSet.ids)
	if !ap.Enabled || na == 0 {
		v.auraOrder = v.auraOrder[:0]
		v.auraKey = auraCacheKey{}
		return
	}
	key := auraCacheKey{cam: v.cam, w: w, h: h, params: ap.key(), hash: v.auraSet.hash, hiddenVer: v.hiddenVer}
	drifted := v.auraDrift*v.cam.zoom >= auraDriftCells*ap.CellSize
	if !v.auraDirty && !drifted && key == v.auraKey {
		return
	}
	v.auraKey, v.auraDirty, v.auraDrift = key, false, 0
	v.auraF.compute(&v.g, v.cam, &v.auraSet, v.hiddenAuras, v.style.NodeRadius, w, h, ap)
	if cap(v.auraRings) < na {
		v.auraRings = slices.Grow(v.auraRings, na-len(v.auraRings))
	}
	v.auraRings = v.auraRings[:na]
	v.auraOrder = v.auraOrder[:0]
	for k := range na {
		v.auraRings[k].reset()
		v.auraF.contours(int32(k), &v.auraRings[k])
		v.auraOrder = append(v.auraOrder, int32(k))
	}
	z := func(k int32) int32 { return ap.Styles[v.auraSet.ids[k]].ZIndex }
	slices.SortStableFunc(v.auraOrder, func(a, b int32) int { return cmp.Compare(z(a), z(b)) })
}

// paintAuras emits every aura's rings in paint order: one concave fill per
// ring with a hairline of the fill colour, or the style's line.
func (v *View) paintAuras() {
	ap := &v.Opts.Auras
	for _, k := range v.auraOrder {
		id := v.auraSet.ids[k]
		fill := ap.fill(int(k), id)
		lineCol, lineW := fill, float32(styletokens.StrokeHair)
		if st, ok := ap.Styles[id]; ok && st.Line.Kind() != color.ColorKindNone {
			lineCol = st.Line
			lineW = st.LineWidth
			if lineW <= 0 {
				lineW = styletokens.StrokeRegular
			}
		}
		r := &v.auraRings[k]
		for i := range r.count() {
			xs, ys := r.ring(i)
			c.PaintPolygonFilled(xs, ys, fill).Concave().Stroke(lineCol, lineW).Send()
		}
	}
}

// emitAuraLegend rebuilds the legend rows — every aura not opted out, in id
// order, hidden ones dimmed — and, when the legend is on, paints them and
// stamps their regions. The rows are kept for next frame's Read.
func (v *View) emitAuraLegend(ap AuraParams) {
	v.legendItems = v.legendItems[:0]
	if !ap.Enabled || !ap.Legend {
		return
	}
	for k, id := range v.auraSet.ids {
		st := ap.Styles[id]
		if st.NoLegend {
			continue
		}
		label := st.Label
		if label == "" {
			label = id
		}
		v.legendItems = append(v.legendItems, legend.Item{
			Key: id, Label: label, Color: opaque(ap.fill(k, id)), Hidden: v.AuraHidden(id),
		})
	}
	if len(v.legendItems) == 0 {
		return
	}
	legend.Paint(v.legendItems, auraLegendInset, auraLegendInset, ap.LegendStyle)
	legend.EmitSense(v.ids, auraLegendPrefix, v.legendItems, auraLegendInset, auraLegendInset, ap.LegendStyle)
}
