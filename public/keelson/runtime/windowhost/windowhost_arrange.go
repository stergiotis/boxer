package windowhost

import (
	"math"
	"slices"

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
// outer rect and its stacking rank (larger is further front).
type arrangeItem struct {
	key  WindowKeyT
	rect Rect
	z    int
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
			x = min(x, work.MaxX-w)
			out[i] = rectAt(x, y, w, h)
			ok[i] = true
		}
	case ArrangeTile:
		cols := int(math.Ceil(math.Sqrt(float64(n))))
		rows := (n + cols - 1) / cols
		cellH := (work.H() - p.gap*float32(rows+1)) / float32(rows)
		// Front window first: the one the user was working in lands
		// top-left.
		for rank := 0; rank < n; rank++ {
			i := order[n-1-rank]
			row := rank / cols
			inRow := cols
			if row == rows-1 {
				inRow = n - row*cols
			}
			col := rank % cols
			cellW := (work.W() - p.gap*float32(inRow+1)) / float32(inRow)
			x := work.MinX + p.gap + float32(col)*(cellW+p.gap)
			y := work.MinY + p.gap + float32(row)*(cellH+p.gap)
			out[i] = rectAt(x, y, cellW, cellH)
			ok[i] = true
		}
	case ArrangeColumns, ArrangeRows:
		span := work.W()
		if cmd == ArrangeRows {
			span = work.H()
		}
		cell := (span - p.gap*float32(n+1)) / float32(n)
		for rank := 0; rank < n; rank++ {
			i := order[n-1-rank]
			off := p.gap + float32(rank)*(cell+p.gap)
			if cmd == ArrangeColumns {
				out[i] = rectAt(work.MinX+off, work.MinY+p.gap, cell, work.H()-2*p.gap)
			} else {
				out[i] = rectAt(work.MinX+p.gap, work.MinY+off, work.W()-2*p.gap, cell)
			}
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
	inst.mu.Lock()
	inst.pendingArrange = cmd
	inst.mu.Unlock()
}

// planArrange computes cmd over the windows' last reported geometry and
// marks each moved window for a one-frame placement. A window that has not
// reported yet (opened this frame) keeps its default placement. Arranging
// ends a maximized window's maximization: the placement is its new rect.
// Render-thread only.
func (inst *Inst) planArrange(cmd ArrangeE, snapshot []*window) {
	sm := c.CurrentApplicationState.StateManager
	work, ok := sm.GetWindowWorkArea()
	if !ok {
		return
	}
	items := make([]arrangeItem, 0, len(snapshot))
	ws := make([]*window, 0, len(snapshot))
	for _, w := range snapshot {
		g, has := sm.GetWindowGeom(w.focusHandle)
		if !has || w.closeReq {
			continue
		}
		items = append(items, arrangeItem{
			key:  w.key,
			rect: Rect{MinX: g.MinX, MinY: g.MinY, MaxX: g.MaxX, MaxY: g.MaxY},
			z:    int(g.Z),
		})
		ws = append(ws, w)
	}
	out, placed := arrangeRects(cmd,
		Rect{MinX: work.MinX, MinY: work.MinY, MaxX: work.MaxX, MaxY: work.MaxY},
		items, defaultArrangeParams)
	for i, w := range ws {
		if !placed[i] {
			continue
		}
		w.place = out[i]
		w.placePending = true
		w.maximized = false
	}
}
