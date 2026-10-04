package windowhost

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// WindowGeom is a window's geometry and shell state as the host last saw
// it, for readers off the render thread (keelson('windows'), ADR-0276
// §SD1). Coordinates are egui logical points with a viewport top-left
// origin.
type WindowGeom struct {
	// Shown is false until the window has reported a frame (it opened this
	// frame); the other geometry fields are zero then.
	Shown bool
	// Rect is the outer rect.
	Rect Rect
	// NeedW, NeedH are the outer size the content needed as laid out
	// (ADR-0275 §SD4): larger than Rect where the content overflowed.
	NeedW, NeedH float32
	// Stack is the stacking rank: larger is further front, 0 unknown.
	Stack     uint32
	Collapsed bool
	Maximized bool
	// Active is the shell's active window (pickActiveWindow).
	Active bool
}

// DesktopInfo is the desktop's state as the host last saw it.
type DesktopInfo struct {
	// WorkShown is false until a frame has shown a window; Work is zero
	// then.
	WorkShown bool
	// Work is the rect the shell's panels leave free: what a maximized
	// window fills and arrangements lay out into.
	Work Rect
	// ActiveKey is the shell's active window, 0 when none.
	ActiveKey WindowKeyT
	// Arranging names the arrangement in progress, ArrangeNone when none.
	Arranging ArrangeE
}

// DesktopInfo returns the desktop's state as of the last completed frame.
// Safe off the render thread.
func (inst *Inst) DesktopInfo() (d DesktopInfo) {
	inst.mu.Lock()
	d = inst.desktop
	inst.mu.Unlock()
	return
}

// snapshotGeometry copies what the host knows of each window and of the
// desktop, as reported by the last completed frame, to where readers off
// the render thread take it under inst.mu. Render-thread only.
func (inst *Inst) snapshotGeometry(snapshot []*window) {
	sm := c.CurrentApplicationState.StateManager
	geoms := make([]WindowGeom, len(snapshot))
	for i, w := range snapshot {
		g := WindowGeom{
			Maximized: w.maximized,
			Active:    w.key == inst.activeKey,
		}
		if v, ok := sm.GetWindowGeom(w.focusHandle); ok {
			g.Shown = true
			g.Rect = Rect{MinX: v.MinX, MinY: v.MinY, MaxX: v.MaxX, MaxY: v.MaxY}
			g.NeedW, g.NeedH = v.NeedW, v.NeedH
			g.Stack = v.Z
			g.Collapsed = v.Collapsed
		}
		geoms[i] = g
	}
	d := DesktopInfo{ActiveKey: inst.activeKey}
	if work, ok := sm.GetWindowWorkArea(); ok {
		d.WorkShown = true
		d.Work = Rect{MinX: work.MinX, MinY: work.MinY, MaxX: work.MaxX, MaxY: work.MaxY}
	}
	if inst.arranging != nil {
		d.Arranging = inst.arranging.cmd
	}
	inst.mu.Lock()
	for i, w := range snapshot {
		w.geom = geoms[i]
	}
	inst.desktop = d
	inst.mu.Unlock()
}
