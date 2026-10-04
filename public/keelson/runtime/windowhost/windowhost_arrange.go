package windowhost

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// ArrangeE names a whole-desktop window arrangement command. Each command is
// a pure function of the work area and the open windows' last reported
// geometry and stacking (arrangeRects); the host applies the result as a
// one-frame placement per window (c.WindowPlace).
type ArrangeE uint8

const (
	ArrangeNone ArrangeE = iota
	// ArrangeCascade stacks the windows diagonally from the work area's
	// top-left corner in stacking order, the front window last, each
	// offset by one title-bar step so every title stays reachable.
	ArrangeCascade
	// ArrangeTile fills the work area with a near-square grid, front window
	// first. A short last row stretches its windows across the full width.
	ArrangeTile
	// ArrangeColumns places the windows side by side, full height.
	ArrangeColumns
	// ArrangeRows stacks the windows top to bottom, full width.
	ArrangeRows
	// ArrangeGather moves every window that lies partly outside the work
	// area back into it, shrinking it only when it is larger than the work
	// area. Windows already inside are left alone.
	ArrangeGather
)

// ArrangeCommands lists the commands in menu order.
var ArrangeCommands = []ArrangeE{ArrangeCascade, ArrangeTile, ArrangeColumns, ArrangeRows, ArrangeGather}

func (inst ArrangeE) String() (s string) {
	switch inst {
	case ArrangeCascade:
		s = "Cascade"
	case ArrangeTile:
		s = "Tile"
	case ArrangeColumns:
		s = "Side by side"
	case ArrangeRows:
		s = "Stacked"
	case ArrangeGather:
		s = "Gather into view"
	default:
		s = "None"
	}
	return
}

// Ident is the command's stable name for callers that name it in text — an
// agent's tool call (ADR-0276 §SD3); String is its menu label.
func (inst ArrangeE) Ident() (s string) {
	switch inst {
	case ArrangeCascade:
		s = "cascade"
	case ArrangeTile:
		s = "tile"
	case ArrangeColumns:
		s = "columns"
	case ArrangeRows:
		s = "rows"
	case ArrangeGather:
		s = "gather"
	}
	return
}

// ParseArrange is the command Ident names, ArrangeNone for any other name.
func ParseArrange(ident string) (cmd ArrangeE) {
	for _, c := range ArrangeCommands {
		if c.Ident() == ident {
			return c
		}
	}
	return ArrangeNone
}

// Rect is an axis-aligned rectangle in egui logical points, viewport
// top-left origin.
type Rect struct {
	MinX, MinY, MaxX, MaxY float32
}

func (inst Rect) W() float32 { return inst.MaxX - inst.MinX }
func (inst Rect) H() float32 { return inst.MaxY - inst.MinY }

func (inst Rect) valid() bool {
	return inst.W() > 0 && inst.H() > 0 &&
		!math.IsNaN(float64(inst.MinX)) && !math.IsNaN(float64(inst.MinY))
}

func rectAt(x, y, w, h float32) Rect {
	return Rect{MinX: x, MinY: y, MaxX: x + w, MaxY: y + h}
}

// arrangeItem is one window as the arrangement sees it: its last reported
// outer rect, its stacking rank (larger is further front) and the smallest
// outer size it accepts (0 = none known).
type arrangeItem struct {
	key        WindowKeyT
	rect       Rect
	z          int
	minW, minH float32
}

// fitSpans lays n spans along a line of length total, separated from each
// other and from both ends by gap, each at least its minimum. Space left
// after the minimums is shared so that spans come out as equal as their
// minimums allow. When the minimums do not fit, the spans keep them and
// overlap their neighbours evenly, still ending inside the line; a minimum
// longer than the whole line is cut to it.
func fitSpans(total, gap float32, mins []float32) (offs, sizes []float32) {
	n := len(mins)
	offs = make([]float32, n)
	sizes = make([]float32, n)
	if n == 0 {
		return
	}
	inner := max(total-2*gap, 1)
	avail := max(inner-gap*float32(n-1), 1)
	var sumMins float32
	for i, m := range mins {
		sizes[i] = min(max(m, 0), inner)
		sumMins += sizes[i]
	}
	if sumMins <= avail {
		// Water filling: a span whose minimum exceeds the equal share keeps
		// its minimum; the others split what remains.
		fixed := make([]bool, n)
		for {
			rest, free := avail, 0
			for i := range sizes {
				if fixed[i] {
					rest -= sizes[i]
				} else {
					free++
				}
			}
			if free == 0 {
				break
			}
			share := rest / float32(free)
			changed := false
			for i := range sizes {
				if !fixed[i] && sizes[i] > share {
					fixed[i] = true
					changed = true
				}
			}
			if changed {
				continue
			}
			for i := range sizes {
				if !fixed[i] {
					sizes[i] = share
				}
			}
			break
		}
	}
	step := gap
	if sumMins > avail && n > 1 {
		step = gap - (sumMins-avail)/float32(n-1)
	}
	off := gap
	for i := range sizes {
		offs[i] = min(off, total-gap-sizes[i])
		off += sizes[i] + step
	}
	return
}

// arrangeParams are the spacing constants of the arrangement.
type arrangeParams struct {
	// gap separates tiled windows from each other and from the work
	// area's edges.
	gap float32
	// cascadeStep is the diagonal offset between cascaded windows — about
	// one title bar, so each title stays visible.
	cascadeStep float32
	// cascadeFrac is the cascaded window size as a fraction of the work
	// area.
	cascadeFrac float32
}

var defaultArrangeParams = arrangeParams{gap: 4, cascadeStep: 28, cascadeFrac: 0.6}

// arrangeRects computes the target rect per item. out is indexed like
// items; ok[i] false leaves item i where it is (ArrangeGather on a window
// already inside, or an item without a usable rect for a command that
// needs one). An empty or invalid work area places nothing.
func arrangeRects(cmd ArrangeE, work Rect, items []arrangeItem, p arrangeParams) (out []Rect, ok []bool) {
	out = make([]Rect, len(items))
	ok = make([]bool, len(items))
	if len(items) == 0 || !work.valid() {
		return
	}
	// Stacking order, back to front; ties fall back to the window key, which
	// is open order.
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if items[a].z != items[b].z {
			return items[a].z - items[b].z
		}
		return int(items[a].key) - int(items[b].key)
	})
	n := len(items)
	switch cmd {
	case ArrangeCascade:
		w := max(work.W()*p.cascadeFrac, 1)
		h := max(work.H()*p.cascadeFrac, 1)
		// Steps that fit before a window would leave the work area; wrap
		// back to the corner after that, shifted right by one column.
		fitX := int((work.W() - w) / p.cascadeStep)
		fitY := int((work.H() - h) / p.cascadeStep)
		perRun := max(min(fitX, fitY)+1, 1)
		for rank, i := range order {
			run, step := rank/perRun, rank%perRun
			x := work.MinX + float32(step)*p.cascadeStep + float32(run)*p.cascadeStep*2
			y := work.MinY + float32(step)*p.cascadeStep
			wi := min(max(w, items[i].minW), work.W())
			hi := min(max(h, items[i].minH), work.H())
			x = max(min(x, work.MaxX-wi), work.MinX)
			y = max(min(y, work.MaxY-hi), work.MinY)
			out[i] = rectAt(x, y, wi, hi)
			ok[i] = true
		}
	case ArrangeTile:
		cols := int(math.Ceil(math.Sqrt(float64(n))))
		rows := (n + cols - 1) / cols
		// Front window first: the one the user was working in lands
		// top-left. A row is as tall as its tallest minimum asks.
		rowMins := make([]float32, rows)
		for rank := 0; rank < n; rank++ {
			row := rank / cols
			rowMins[row] = max(rowMins[row], items[order[n-1-rank]].minH)
		}
		rowOffs, rowHs := fitSpans(work.H(), p.gap, rowMins)
		for row := 0; row < rows; row++ {
			first := row * cols
			inRow := min(cols, n-first)
			colMins := make([]float32, inRow)
			for col := range colMins {
				colMins[col] = items[order[n-1-first-col]].minW
			}
			colOffs, colWs := fitSpans(work.W(), p.gap, colMins)
			for col := range colMins {
				i := order[n-1-first-col]
				out[i] = rectAt(work.MinX+colOffs[col], work.MinY+rowOffs[row], colWs[col], rowHs[row])
				ok[i] = true
			}
		}
	case ArrangeColumns, ArrangeRows:
		mins := make([]float32, n)
		for rank := range mins {
			it := items[order[n-1-rank]]
			if cmd == ArrangeColumns {
				mins[rank] = it.minW
			} else {
				mins[rank] = it.minH
			}
		}
		if cmd == ArrangeColumns {
			offs, ws := fitSpans(work.W(), p.gap, mins)
			for rank := range mins {
				out[order[n-1-rank]] = rectAt(work.MinX+offs[rank], work.MinY+p.gap, ws[rank], work.H()-2*p.gap)
			}
		} else {
			offs, hs := fitSpans(work.H(), p.gap, mins)
			for rank := range mins {
				out[order[n-1-rank]] = rectAt(work.MinX+p.gap, work.MinY+offs[rank], work.W()-2*p.gap, hs[rank])
			}
		}
		for i := range ok {
			ok[i] = true
		}
	case ArrangeGather:
		for i, it := range items {
			r := it.rect
			if !r.valid() {
				continue
			}
			if r.MinX >= work.MinX && r.MinY >= work.MinY && r.MaxX <= work.MaxX && r.MaxY <= work.MaxY {
				continue
			}
			w := min(r.W(), work.W())
			h := min(r.H(), work.H())
			x := min(max(r.MinX, work.MinX), work.MaxX-w)
			y := min(max(r.MinY, work.MinY), work.MaxY-h)
			out[i] = rectAt(x, y, w, h)
			ok[i] = true
		}
	default:
	}
	return
}

// Arrange queues a whole-desktop arrangement for the next Frame. Safe off
// the render thread; a second call before that Frame replaces the first.
func (inst *Inst) Arrange(cmd ArrangeE) {
	_ = inst.ArrangeWindows(cmd, nil)
}

// ArrangeWindows queues cmd over the windows keys names, or over every
// window when keys is empty, for the next Frame. Windows left out stay
// where they are; the named ones are laid out in the work area as if they
// were the only ones. A key that names no open window refuses the whole
// request, so nothing moves on a partly wrong list. Safe off the render
// thread; a second call before that Frame replaces the first.
func (inst *Inst) ArrangeWindows(cmd ArrangeE, keys []WindowKeyT) (err error) {
	if cmd == ArrangeNone || int(cmd) > len(ArrangeCommands) {
		err = eb.Build().Uint8("cmd", uint8(cmd)).Errorf("windowhost: unknown arrangement")
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, k := range keys {
		if inst.openWindowLocked(k) == nil {
			err = eb.Build().Uint64("key", uint64(k)).Errorf("windowhost: no open window with this key")
			return
		}
	}
	inst.pendingArrange = cmd
	inst.pendingArrangeKeys = slices.Clone(keys)
	return
}

// Raise brings the window key names to the front on the next Frame. Safe
// off the render thread.
func (inst *Inst) Raise(key WindowKeyT) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.openWindowLocked(key) == nil {
		err = eb.Build().Uint64("key", uint64(key)).Errorf("windowhost: no open window with this key")
		return
	}
	inst.pendingRaise = key
	return
}

// Place sets the outer rect of the window key names on the next Frame, as
// a one-frame placement (ADR-0275 §SD3): the window is movable and
// resizable again afterwards, and egui sizes it to its content on an axis
// the content does not fill. Placing ends the window's maximization and
// any arrangement in progress, whose next pass would move it again. Safe
// off the render thread.
func (inst *Inst) Place(key WindowKeyT, r Rect) (err error) {
	if !r.valid() {
		err = eb.Build().Float32("w", r.W()).Float32("h", r.H()).Errorf("windowhost: a placement needs a positive size")
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.openWindowLocked(key) == nil {
		err = eb.Build().Uint64("key", uint64(key)).Errorf("windowhost: no open window with this key")
		return
	}
	if inst.pendingPlaces == nil {
		inst.pendingPlaces = make(map[WindowKeyT]Rect, 1)
	}
	inst.pendingPlaces[key] = r
	return
}

// openWindowLocked returns the open, not closing, window key names, or nil.
// inst.mu must be held.
func (inst *Inst) openWindowLocked(key WindowKeyT) *window {
	for _, w := range inst.windows {
		if w.key == key && !w.closeReq {
			return w
		}
	}
	return nil
}

// applyPlaces marks each window with a queued placement for it. A
// placement ends an arrangement in progress. Render-thread only.
func (inst *Inst) applyPlaces(places map[WindowKeyT]Rect, snapshot []*window) {
	if len(places) == 0 {
		return
	}
	inst.arranging = nil
	for _, w := range snapshot {
		if r, ok := places[w.key]; ok {
			w.place = r
			w.placePending = true
			w.maximized = false
		}
	}
}

// minArrangePasses is the least number of re-runs an arrangement may take
// after windows report that their content needed more than they were placed
// at. A run allows one per window besides, since each pass can find another
// window's minimum once the minimums found so far squeeze it.
const minArrangePasses = 3

// needSlack absorbs float noise when comparing a window's needed size with
// the size it was placed at, in logical points.
const needSlack = 0.5

// arrangeRun is an arrangement in progress. egui widens a window to what its
// content needs, and that need is only known once the content has been laid
// out at the placed size; so a run places, reads back each placed window's
// need a frame later, and places again with the needs as minimums until
// nothing grows or the passes run out. Render-thread only.
type arrangeRun struct {
	cmd ArrangeE
	// keys limits the run to these windows; nil is every window.
	keys   map[WindowKeyT]bool
	placed map[WindowKeyT]Rect
	mins   map[WindowKeyT][2]float32
	passes int
}

// stepArrange starts the arrangement cmd over keys (when cmd is not
// ArrangeNone; empty keys is every window) or advances the one in progress.
func (inst *Inst) stepArrange(cmd ArrangeE, keys []WindowKeyT, snapshot []*window) {
	sm := c.CurrentApplicationState.StateManager
	if cmd != ArrangeNone {
		var only map[WindowKeyT]bool
		if len(keys) > 0 {
			only = make(map[WindowKeyT]bool, len(keys))
			for _, k := range keys {
				only[k] = true
			}
		}
		inst.arranging = &arrangeRun{
			keys:   only,
			cmd:    cmd,
			placed: make(map[WindowKeyT]Rect, len(snapshot)),
			mins:   make(map[WindowKeyT][2]float32, len(snapshot)),
			passes: max(minArrangePasses, len(snapshot)+1),
		}
		inst.planArrange(snapshot)
		return
	}
	run := inst.arranging
	if run == nil {
		return
	}
	grew := false
	for _, w := range snapshot {
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
		inst.arranging = nil
		return
	}
	inst.planArrange(snapshot)
	if run.passes <= 0 {
		// Out of passes: this placement uses everything learned, and
		// nothing reads back what it needs.
		inst.arranging = nil
	}
}

// planArrange computes the run's command over the windows' last reported
// geometry and the minimums learned so far, and marks each moved window for
// a one-frame placement. A window that has not reported yet (opened this
// frame) keeps its default placement. Arranging ends a maximized window's
// maximization: the placement is its new rect.
func (inst *Inst) planArrange(snapshot []*window) {
	run := inst.arranging
	sm := c.CurrentApplicationState.StateManager
	work, ok := sm.GetWindowWorkArea()
	if !ok {
		inst.arranging = nil
		return
	}
	items := make([]arrangeItem, 0, len(snapshot))
	ws := make([]*window, 0, len(snapshot))
	for _, w := range snapshot {
		if run.keys != nil && !run.keys[w.key] {
			continue
		}
		g, has := sm.GetWindowGeom(w.focusHandle)
		if !has || w.closeReq {
			continue
		}
		m := run.mins[w.key]
		items = append(items, arrangeItem{
			key:  w.key,
			rect: Rect{MinX: g.MinX, MinY: g.MinY, MaxX: g.MaxX, MaxY: g.MaxY},
			z:    int(g.Z),
			minW: m[0],
			minH: m[1],
		})
		ws = append(ws, w)
	}
	out, placed := arrangeRects(run.cmd,
		Rect{MinX: work.MinX, MinY: work.MinY, MaxX: work.MaxX, MaxY: work.MaxY},
		items, defaultArrangeParams)
	for i, w := range ws {
		if !placed[i] {
			continue
		}
		w.place = out[i]
		w.placePending = true
		w.maximized = false
		run.placed[w.key] = out[i]
	}
}
