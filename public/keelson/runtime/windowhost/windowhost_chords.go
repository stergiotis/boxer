package windowhost

import (
	"slices"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// The shell's chords (ADR-0346 §SD5). Rust drains the Alt chords of a frame
// into one bit set and consumes the events; the host, the one poller, reads
// the set at the start of its Frame and acts: focus by order or direction,
// swap, promote, snap, the layout and its parameters. A focus is a raise —
// the stacking report a frame later makes the raised window the active one,
// as OpenOrRaise already relies on.

// snapState remembers the last snap so a second press of the same chord on
// the same window steps from the half to the quarters.
type snapState struct {
	key   WindowKeyT
	chord keycodes.ShellChordBit
	stage int
}

// stepChords acts on the chords pressed since the last frame. Render-thread
// only; runs before Frame takes its snapshot, so a raise or a placement it
// queues lands in the same frame.
func (inst *Inst) stepChords(sm *c.StateManager) {
	set := sm.GetShellChords()
	if set == 0 {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	open := make([]*window, 0, len(inst.windows))
	for _, w := range inst.windows {
		if !w.closeReq {
			open = append(open, w)
		}
	}
	if len(open) == 0 {
		return
	}
	active := inst.openWindowLocked(inst.activeKey)
	tiled := inst.layout.Tiled()
	raise := func(key WindowKeyT) {
		if key != 0 {
			inst.pendingRaise = key
		}
	}

	// Focus along the order: the layout's order on a tiled desktop, the
	// stacking (front first) on a floating one.
	if keycodes.ChordFocusNext.Has(set) || keycodes.ChordFocusPrev.Has(set) {
		ring := inst.focusRingLocked(open, tiled)
		if i := slices.Index(ring, inst.activeKey); i >= 0 && len(ring) > 1 {
			step := 1
			if keycodes.ChordFocusPrev.Has(set) {
				step = len(ring) - 1
			}
			raise(ring[(i+step)%len(ring)])
		} else if len(ring) > 0 {
			raise(ring[0])
		}
	}
	// Focus by direction, from the active window's rect.
	for _, d := range []struct {
		chord  keycodes.ShellChordBit
		dx, dy float32
	}{
		{keycodes.ChordFocusLeft, -1, 0}, {keycodes.ChordFocusRight, 1, 0},
		{keycodes.ChordFocusUp, 0, -1}, {keycodes.ChordFocusDown, 0, 1},
	} {
		if d.chord.Has(set) {
			raise(nearestInDirection(open, active, d.dx, d.dy))
		}
	}
	if active == nil {
		return
	}
	if keycodes.ChordPrevActive.Has(set) {
		if inst.openWindowLocked(inst.prevActiveKey) != nil {
			raise(inst.prevActiveKey)
		}
	}

	// The order and the layout.
	if tiled {
		if keycodes.ChordSwapNext.Has(set) {
			inst.swapInOrderLocked(active.key, 1)
		}
		if keycodes.ChordSwapPrev.Has(set) {
			inst.swapInOrderLocked(active.key, -1)
		}
		if keycodes.ChordPromote.Has(set) {
			if i := slices.Index(inst.tileOrder, active.key); i > 0 {
				inst.tileOrder = slices.Delete(inst.tileOrder, i, i+1)
				inst.tileOrder = slices.Insert(inst.tileOrder, 0, active.key)
				inst.tileDirty = true
			}
		}
		if keycodes.ChordToggleFloat.Has(set) {
			inst.setFloatingLocked(active.key, !inst.floating[active.key])
		}
		if keycodes.ChordMonocle.Has(set) {
			if inst.layout == LayoutMonocle {
				// Back to the tiled layout monocle was entered from; a
				// desktop that started in monocle goes to the first.
				prev := inst.layoutBeforeMonocle
				if !prev.Tiled() || prev == LayoutMonocle {
					prev = nextTiledLayout(LayoutFloating)
				}
				inst.setLayoutLocked(prev)
			} else {
				inst.setLayoutLocked(LayoutMonocle)
			}
		}
	} else {
		if keycodes.ChordMonocle.Has(set) {
			active.maximized = !active.maximized
		}
		for _, d := range []struct {
			chord  keycodes.ShellChordBit
			dx, dy int
		}{
			{keycodes.ChordSnapLeft, -1, 0}, {keycodes.ChordSnapRight, 1, 0},
			{keycodes.ChordSnapUp, 0, -1}, {keycodes.ChordSnapDown, 0, 1},
		} {
			if d.chord.Has(set) {
				inst.snapLocked(active, d.chord, d.dx, d.dy)
			}
		}
	}
	if keycodes.ChordCycleLayout.Has(set) {
		inst.setLayoutLocked(nextTiledLayout(inst.layout))
	}
	if !tiled {
		// The parameters are the layouts'; on a floating desktop the chords
		// mean nothing and are dropped (ADR-0346 §SD5).
		return
	}
	p := inst.layoutParams
	if keycodes.ChordRatioUp.Has(set) {
		p.MainRatio += MainRatioStep
	}
	if keycodes.ChordRatioDown.Has(set) {
		p.MainRatio -= MainRatioStep
	}
	if keycodes.ChordCountUp.Has(set) {
		p.MainCount++
	}
	if keycodes.ChordCountDown.Has(set) {
		p.MainCount--
	}
	if p = p.clamped(); p != inst.layoutParams {
		inst.layoutParams = p
		inst.tileDirty = true
	}
}

// setLayoutLocked is SetLayout under inst.mu. Entering monocle from another
// tiled layout remembers that layout, whichever way monocle was chosen, so
// Alt+F can return to it.
func (inst *Inst) setLayoutLocked(l LayoutE) {
	if int(l) >= len(Layouts) || inst.layout == l {
		return
	}
	if l == LayoutMonocle && inst.layout.Tiled() {
		inst.layoutBeforeMonocle = inst.layout
	}
	inst.layout = l
	inst.tileDirty = true
}

// nextTiledLayout is the tiled layout after l in menu order, wrapping among
// the tiled ones; the first tiled layout for a floating desktop.
func nextTiledLayout(l LayoutE) LayoutE {
	tiled := Layouts[1:]
	if i := slices.Index(tiled, l); i >= 0 {
		return tiled[(i+1)%len(tiled)]
	}
	return tiled[0]
}

// focusRingLocked is the keys to cycle focus through: the order on a tiled
// desktop, the stacking front first on a floating one. inst.mu must be held.
func (inst *Inst) focusRingLocked(open []*window, tiled bool) (ring []WindowKeyT) {
	if tiled {
		return slices.Clone(inst.tileOrder)
	}
	ws := slices.Clone(open)
	slices.SortStableFunc(ws, func(a, b *window) int {
		if a.geom.Stack != b.geom.Stack {
			return int(b.geom.Stack) - int(a.geom.Stack)
		}
		return int(b.key) - int(a.key)
	})
	ring = make([]WindowKeyT, len(ws))
	for i, w := range ws {
		ring[i] = w.key
	}
	return
}

// swapInOrderLocked swaps the window with its neighbour step places along
// the order, wrapping. inst.mu must be held.
func (inst *Inst) swapInOrderLocked(key WindowKeyT, step int) {
	n := len(inst.tileOrder)
	i := slices.Index(inst.tileOrder, key)
	if i < 0 || n < 2 {
		return
	}
	j := ((i+step)%n + n) % n
	inst.tileOrder[i], inst.tileOrder[j] = inst.tileOrder[j], inst.tileOrder[i]
	inst.tileDirty = true
}

// nearestInDirection is the window whose centre lies nearest to from's in
// the direction (dx, dy), by the last reported rects; 0 when none lies that
// way or from has no rect yet. Distance along the direction counts in full,
// across it by half, so a window straight ahead wins over one off to the
// side at the same distance.
func nearestInDirection(open []*window, from *window, dx, dy float32) (key WindowKeyT) {
	if from == nil || !from.geom.Shown {
		return 0
	}
	fx := (from.geom.Rect.MinX + from.geom.Rect.MaxX) / 2
	fy := (from.geom.Rect.MinY + from.geom.Rect.MaxY) / 2
	best := float32(-1)
	for _, w := range open {
		if w == from || !w.geom.Shown {
			continue
		}
		cx := (w.geom.Rect.MinX+w.geom.Rect.MaxX)/2 - fx
		cy := (w.geom.Rect.MinY+w.geom.Rect.MaxY)/2 - fy
		along := cx*dx + cy*dy
		if along <= 0.5 {
			continue
		}
		across := cx*dy - cy*dx
		if across < 0 {
			across = -across
		}
		d := along + across/2
		if best < 0 || d < best {
			best, key = d, w.key
		}
	}
	return
}

// snapLocked places the window in the half of the work area the direction
// names; a second press of the same chord on the same window steps through
// that half's two quarters and back. inst.mu must be held.
func (inst *Inst) snapLocked(w *window, chord keycodes.ShellChordBit, dx, dy int) {
	if !inst.desktop.WorkShown {
		return
	}
	stage := 0
	if inst.snap.key == w.key && inst.snap.chord == chord {
		stage = (inst.snap.stage + 1) % 3
	}
	inst.snap = snapState{key: w.key, chord: chord, stage: stage}
	work := inst.desktop.Work
	gap := defaultArrangeParams.gap
	halfW, halfH := (work.W()-3*gap)/2, (work.H()-3*gap)/2
	// The half the direction names, then its quarters along the other axis.
	r := Rect{MinX: work.MinX + gap, MinY: work.MinY + gap, MaxX: work.MaxX - gap, MaxY: work.MaxY - gap}
	switch {
	case dx < 0:
		r.MaxX = r.MinX + halfW
	case dx > 0:
		r.MinX = r.MaxX - halfW
	case dy < 0:
		r.MaxY = r.MinY + halfH
	case dy > 0:
		r.MinY = r.MaxY - halfH
	}
	switch {
	case stage == 1 && dx != 0:
		r.MaxY = r.MinY + halfH
	case stage == 2 && dx != 0:
		r.MinY = r.MaxY - halfH
	case stage == 1 && dy != 0:
		r.MaxX = r.MinX + halfW
	case stage == 2 && dy != 0:
		r.MinX = r.MaxX - halfW
	}
	if !r.valid() {
		return
	}
	if inst.pendingPlaces == nil {
		inst.pendingPlaces = make(map[WindowKeyT]Rect, 1)
	}
	inst.pendingPlaces[w.key] = r
}
