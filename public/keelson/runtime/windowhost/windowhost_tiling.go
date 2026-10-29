package windowhost

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The tiled desktop (ADR-0346). A layout is a pure function of the ordered
// tiled windows, the work area and a few parameters (tileRects); the host
// keeps it live by pinning every tiled window to its cell each frame through
// the one-frame placement of ADR-0275 §SD3, and re-plans when an input
// changes. egui grows a pinned window around its content rather than holding
// its cell (ADR-0346, update of 2026-10-29), so each re-plan is a settling
// run: place, read back what each window needed, place again with the needs
// as minimums, until nothing grows or the passes run out.

// LayoutE names the desktop's layout. LayoutFloating is the floating desktop
// every host starts with; the others tile.
type LayoutE uint8

const (
	LayoutFloating LayoutE = iota
	// LayoutMainStack gives the main windows a column at the main ratio and
	// stacks the others in the remaining column.
	LayoutMainStack
	// LayoutTile is ArrangeTile kept live: a near-square grid, main first.
	LayoutTile
	// LayoutColumns is ArrangeColumns kept live: side by side, full height.
	LayoutColumns
	// LayoutRows is ArrangeRows kept live: top to bottom, full width.
	LayoutRows
	// LayoutMonocle gives every tiled window the whole work area; the
	// active one is in front.
	LayoutMonocle
)

// Layouts lists the layouts in menu order, the floating desktop first.
var Layouts = []LayoutE{LayoutFloating, LayoutMainStack, LayoutTile, LayoutColumns, LayoutRows, LayoutMonocle}

// Tiled is whether the layout pins windows.
func (inst LayoutE) Tiled() bool { return inst != LayoutFloating && int(inst) < len(Layouts) }

// String is the layout's menu label.
func (inst LayoutE) String() (s string) {
	switch inst {
	case LayoutFloating:
		s = "Floating"
	case LayoutMainStack:
		s = "Main and stack"
	case LayoutTile:
		s = "Tile"
	case LayoutColumns:
		s = "Columns"
	case LayoutRows:
		s = "Rows"
	case LayoutMonocle:
		s = "Monocle"
	default:
		s = "Unknown"
	}
	return
}

// Ident is the layout's stable name for callers that name it in text — the
// env var that seeds it, an agent's tool call; String is its menu label.
func (inst LayoutE) Ident() (s string) {
	switch inst {
	case LayoutFloating:
		s = "floating"
	case LayoutMainStack:
		s = "main-stack"
	case LayoutTile:
		s = "tile"
	case LayoutColumns:
		s = "columns"
	case LayoutRows:
		s = "rows"
	case LayoutMonocle:
		s = "monocle"
	}
	return
}

// LayoutIdents lists every layout's Ident in menu order.
func LayoutIdents() (out []string) {
	out = make([]string, 0, len(Layouts))
	for _, l := range Layouts {
		out = append(out, l.Ident())
	}
	return
}

// ParseLayout is the layout Ident names; ok is false for any other name.
func ParseLayout(ident string) (l LayoutE, ok bool) {
	for _, cand := range Layouts {
		if cand.Ident() == ident {
			return cand, true
		}
	}
	return LayoutFloating, false
}

// LayoutParams are the parameters of the layouts that take any.
type LayoutParams struct {
	// MainRatio is the share of the work area's width the main column takes
	// under LayoutMainStack, within [MainRatioMin, MainRatioMax].
	MainRatio float32
	// MainCount is how many windows, from the front of the order, are main
	// under LayoutMainStack; at least 1.
	MainCount int
}

const (
	MainRatioMin  float32 = 0.2
	MainRatioMax  float32 = 0.8
	MainRatioStep float32 = 0.05
)

// DefaultLayoutParams is what a host starts with.
var DefaultLayoutParams = LayoutParams{MainRatio: 0.6, MainCount: 1}

// clamped brings the parameters into their ranges.
func (inst LayoutParams) clamped() LayoutParams {
	inst.MainRatio = min(max(inst.MainRatio, MainRatioMin), MainRatioMax)
	if inst.MainRatio != inst.MainRatio { // NaN
		inst.MainRatio = DefaultLayoutParams.MainRatio
	}
	inst.MainCount = max(inst.MainCount, 1)
	return inst
}

// tileItem is one tiled window as the layout sees it: its key and the
// smallest outer size it accepts (0 = none known).
type tileItem struct {
	key        WindowKeyT
	minW, minH float32
}

// tileRects computes the cell per item for layout l. items are in order,
// main first. An empty or invalid work area, or a floating layout, places
// nothing.
func tileRects(l LayoutE, work Rect, items []tileItem, lp LayoutParams, ap arrangeParams) (out []Rect) {
	n := len(items)
	out = make([]Rect, n)
	if n == 0 || !work.valid() || !l.Tiled() {
		return
	}
	lp = lp.clamped()
	switch l {
	case LayoutMainStack:
		m := min(lp.MainCount, n)
		if m >= n {
			// Every window is main: one column of rows.
			return tileRects(LayoutRows, work, items, lp, ap)
		}
		minW := func(its []tileItem) (w float32) {
			for _, it := range its {
				w = max(w, it.minW)
			}
			return
		}
		minHs := func(its []tileItem) (hs []float32) {
			hs = make([]float32, len(its))
			for i, it := range its {
				hs[i] = it.minH
			}
			return
		}
		inner := max(work.W()-3*ap.gap, 1)
		minMain, minStack := min(minW(items[:m]), inner), min(minW(items[m:]), inner)
		mainW := max(inner*lp.MainRatio, minMain)
		stackW := max(inner-mainW, minStack)
		if mainW+stackW > inner {
			// The ratio yields before anything overlaps: the main column
			// shrinks towards its minimum to make room for the stack's.
			mainW = max(inner-stackW, minMain)
		}
		mainX := work.MinX + ap.gap
		stackX := mainX + mainW + ap.gap
		if over := mainW + stackW - inner; over > 0 {
			// The minimums do not fit: the stack column overlaps the main
			// column by the excess, still ending inside the work area.
			stackX -= over
		}
		offs, hs := fitSpans(work.H(), ap.gap, minHs(items[:m]))
		for i := range m {
			out[i] = rectAt(mainX, work.MinY+offs[i], mainW, hs[i])
		}
		offs, hs = fitSpans(work.H(), ap.gap, minHs(items[m:]))
		for i := m; i < n; i++ {
			out[i] = rectAt(stackX, work.MinY+offs[i-m], stackW, hs[i-m])
		}
	case LayoutTile, LayoutColumns, LayoutRows:
		// The arrangements order by stacking, front first; the order's
		// front is the main, so the rank stands in for the stacking.
		cmd := ArrangeTile
		switch l {
		case LayoutColumns:
			cmd = ArrangeColumns
		case LayoutRows:
			cmd = ArrangeRows
		default:
		}
		ai := make([]arrangeItem, n)
		for i, it := range items {
			ai[i] = arrangeItem{key: it.key, rect: work, z: n - i, minW: it.minW, minH: it.minH}
		}
		rects, _ := arrangeRects(cmd, work, ai, ap)
		copy(out, rects)
	case LayoutMonocle:
		cell := rectAt(work.MinX+ap.gap, work.MinY+ap.gap, max(work.W()-2*ap.gap, 1), max(work.H()-2*ap.gap, 1))
		for i := range out {
			out[i] = cell
		}
	default:
	}
	return
}

// Layout is the desktop's layout. Safe off the render thread.
func (inst *Inst) Layout() (l LayoutE) {
	inst.mu.Lock()
	l = inst.layout
	inst.mu.Unlock()
	return
}

// SetLayout sets the desktop's layout from the next Frame. Switching to the
// floating desktop leaves every window where the layout last put it. Safe
// off the render thread.
func (inst *Inst) SetLayout(l LayoutE) (err error) {
	if int(l) >= len(Layouts) {
		err = eb.Build().Uint8("layout", uint8(l)).Errorf("windowhost: unknown layout")
		return
	}
	inst.mu.Lock()
	if inst.layout != l {
		inst.layout = l
		inst.tileDirty = true
	}
	inst.mu.Unlock()
	return
}

// LayoutParams are the layouts' parameters. Safe off the render thread.
func (inst *Inst) LayoutParams() (p LayoutParams) {
	inst.mu.Lock()
	p = inst.layoutParams
	inst.mu.Unlock()
	return
}

// SetLayoutParams sets the layouts' parameters, clamped to their ranges, from
// the next Frame. Safe off the render thread.
func (inst *Inst) SetLayoutParams(p LayoutParams) {
	p = p.clamped()
	inst.mu.Lock()
	if inst.layoutParams != p {
		inst.layoutParams = p
		inst.tileDirty = true
	}
	inst.mu.Unlock()
}

// FloatWindow takes the window key names out of the layout (floating true)
// or puts it back as the main (false), from the next Frame. A floating
// window keeps egui's drag and resize. Safe off the render thread.
func (inst *Inst) FloatWindow(key WindowKeyT, floating bool) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.openWindowLocked(key) == nil {
		err = eb.Build().Uint64("key", uint64(key)).Errorf("windowhost: no open window with this key")
		return
	}
	inst.setFloatingLocked(key, floating)
	return
}

// setFloatingLocked is FloatWindow under inst.mu. Unfloating moves the
// window to the front of the order (ADR-0346 §SD4).
func (inst *Inst) setFloatingLocked(key WindowKeyT, floating bool) {
	if inst.floating[key] == floating {
		return
	}
	if floating {
		if inst.floating == nil {
			inst.floating = make(map[WindowKeyT]bool, 1)
		}
		inst.floating[key] = true
	} else {
		delete(inst.floating, key)
		if i := slices.Index(inst.tileOrder, key); i > 0 {
			inst.tileOrder = slices.Delete(inst.tileOrder, i, i+1)
			inst.tileOrder = slices.Insert(inst.tileOrder, 0, key)
		}
	}
	inst.tileDirty = true
}

// WindowFloating is whether the window key names is out of the layout by
// choice (FloatWindow). Safe off the render thread.
func (inst *Inst) WindowFloating(key WindowKeyT) (floating bool) {
	inst.mu.Lock()
	floating = inst.floating[key]
	inst.mu.Unlock()
	return
}

// tileRun is a settling run in progress: the cells placed, and the minimums
// learned from what each placed window needed. Render-thread only.
type tileRun struct {
	placed map[WindowKeyT]Rect
	mins   map[WindowKeyT][2]float32
	passes int
}

// tileInputs is what Frame takes of the tiling state under inst.mu.
type tileInputs struct {
	layout   LayoutE
	params   LayoutParams
	order    []WindowKeyT
	floating map[WindowKeyT]bool
	dirty    bool
}

// takeTileInputs syncs the order with the open windows and copies the tiling
// inputs out from under inst.mu, clearing the dirty flag. A window that
// opened since the last Frame joins the order as the main; one that closed
// leaves it. Either counts as a change.
func (inst *Inst) takeTileInputs(snapshot []*window) (in tileInputs) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	open := make(map[WindowKeyT]bool, len(snapshot))
	for _, w := range snapshot {
		if !w.closeReq {
			open[w.key] = true
		}
	}
	kept := inst.tileOrder[:0]
	for _, k := range inst.tileOrder {
		if open[k] {
			kept = append(kept, k)
		} else {
			inst.tileDirty = true
			delete(inst.floating, k)
		}
	}
	inst.tileOrder = kept
	// New windows, newest first, each in front of the order.
	var fresh []WindowKeyT
	for _, w := range snapshot {
		if open[w.key] && !slices.Contains(inst.tileOrder, w.key) {
			fresh = append(fresh, w.key)
		}
	}
	if len(fresh) > 0 {
		slices.SortFunc(fresh, func(a, b WindowKeyT) int { return int(b) - int(a) })
		inst.tileOrder = append(fresh, inst.tileOrder...)
		inst.tileDirty = true
	}
	in = tileInputs{
		layout:   inst.layout,
		params:   inst.layoutParams,
		order:    slices.Clone(inst.tileOrder),
		floating: make(map[WindowKeyT]bool, len(inst.floating)),
		dirty:    inst.tileDirty,
	}
	for k, v := range inst.floating {
		in.floating[k] = v
	}
	inst.tileDirty = false
	return
}

// tileSlack absorbs float noise when comparing work areas, in logical
// points.
const tileSlack = 0.5

func rectNear(a, b Rect) bool {
	d := func(x, y float32) bool { return x-y < tileSlack && y-x < tileSlack }
	return d(a.MinX, b.MinX) && d(a.MinY, b.MinY) && d(a.MaxX, b.MaxX) && d(a.MaxY, b.MaxY)
}

// stepTiling keeps the layout live: it starts a settling run when an input
// changed, advances the one in progress, and pins every tiled window to its
// cell. Render-thread only; runs after the arrangements and placements of
// the frame, which it overrides on a tiled desktop.
func (inst *Inst) stepTiling(snapshot []*window) {
	in := inst.takeTileInputs(snapshot)
	if !in.layout.Tiled() {
		inst.tiling = nil
		inst.tileCells = nil
		inst.tileMins = nil
		return
	}
	// A tiled desktop has no one-shot arrangements.
	inst.arranging = nil
	sm := c.CurrentApplicationState.StateManager
	work, ok := sm.GetWindowWorkArea()
	if !ok {
		return
	}
	workRect := Rect{MinX: work.MinX, MinY: work.MinY, MaxX: work.MaxX, MaxY: work.MaxY}
	// The windows the layout places this frame, in order: open, not
	// floating by choice or by being maximized, and rendered at least once
	// — a placement is emitted with the window's handle, which its first
	// frame has not produced yet, so a new window takes its cell a frame
	// after it opens.
	byKey := make(map[WindowKeyT]*window, len(snapshot))
	for _, w := range snapshot {
		byKey[w.key] = w
	}
	tiled := make([]*window, 0, len(in.order))
	for _, k := range in.order {
		w := byKey[k]
		if w == nil || w.closeReq || in.floating[k] || w.maximized || w.focusHandle == 0 {
			continue
		}
		tiled = append(tiled, w)
	}
	setChanged := len(tiled) != len(inst.tileCells)
	if !setChanged {
		for _, w := range tiled {
			if _, has := inst.tileCells[w.key]; !has {
				setChanged = true
				break
			}
		}
	}
	workChanged := inst.tileCells != nil && !rectNear(workRect, inst.tileWork)
	switch {
	case in.dirty || setChanged || inst.tileCells == nil:
		// A change of the set, the order, the layout or its parameters:
		// learn the minimums afresh, as an arrangement does.
		inst.tileMins = nil
		inst.startTileRun(in, workRect, tiled)
	case workChanged:
		// Only the work area moved (zoom, density, the viewport): the
		// minimums learned still hold, so the run starts from them rather
		// than squeezing every window again.
		inst.startTileRun(in, workRect, tiled)
	case inst.tiling != nil:
		inst.advanceTileRun(in, workRect, tiled)
	default:
	}
	for _, w := range tiled {
		if cell, has := inst.tileCells[w.key]; has {
			w.place = cell
			w.placePending = true
		}
	}
}

// startTileRun begins a settling run over the tiled windows, seeded with the
// minimums kept in inst.tileMins (nil to learn afresh), and plans once.
func (inst *Inst) startTileRun(in tileInputs, work Rect, tiled []*window) {
	mins := inst.tileMins
	if mins == nil {
		mins = make(map[WindowKeyT][2]float32, len(tiled))
	}
	inst.tiling = &tileRun{
		placed: make(map[WindowKeyT]Rect, len(tiled)),
		mins:   mins,
		passes: max(minArrangePasses, len(tiled)+1),
	}
	inst.tileMins = mins
	inst.tileWork = work
	inst.planTiles(in, work, tiled)
}

// advanceTileRun reads what each placed window needed a frame later, grows
// the minimums, and plans again while something grew and passes remain.
func (inst *Inst) advanceTileRun(in tileInputs, work Rect, tiled []*window) {
	run := inst.tiling
	sm := c.CurrentApplicationState.StateManager
	grew := false
	for _, w := range tiled {
		r, was := run.placed[w.key]
		if !was {
			continue
		}
		g, has := sm.GetWindowGeom(w.focusHandle)
		if !has {
			continue
		}
		m := run.mins[w.key]
		if g.NeedW > r.W()+needSlack && g.NeedW > m[0] {
			m[0] = g.NeedW
			grew = true
		}
		if g.NeedH > r.H()+needSlack && g.NeedH > m[1] {
			m[1] = g.NeedH
			grew = true
		}
		run.mins[w.key] = m
	}
	run.passes--
	if !grew {
		inst.tiling = nil
		return
	}
	inst.planTiles(in, work, tiled)
	if run.passes <= 0 {
		// Out of passes: this plan uses everything learned, and nothing
		// reads back what it needs.
		inst.tiling = nil
	}
}

// planTiles computes the layout over the tiled windows with the minimums
// learned so far and records the cells, which stepTiling pins every frame.
func (inst *Inst) planTiles(in tileInputs, work Rect, tiled []*window) {
	run := inst.tiling
	items := make([]tileItem, len(tiled))
	for i, w := range tiled {
		m := run.mins[w.key]
		items[i] = tileItem{key: w.key, minW: m[0], minH: m[1]}
	}
	rects := tileRects(in.layout, work, items, in.params, defaultArrangeParams)
	cells := make(map[WindowKeyT]Rect, len(tiled))
	for i, w := range tiled {
		if !rects[i].valid() {
			continue
		}
		cells[w.key] = rects[i]
		run.placed[w.key] = rects[i]
	}
	inst.tileCells = cells
}

// tileOrderOf is the window's 1-based position in the order, main first, or
// 0 when it is not in the order. inst.mu must be held.
func (inst *Inst) tileOrderOfLocked(key WindowKeyT) int {
	if i := slices.Index(inst.tileOrder, key); i >= 0 {
		return i + 1
	}
	return 0
}
