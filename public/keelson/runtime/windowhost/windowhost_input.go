package windowhost

import (
	"math"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The person's input, for an idle timer (ADR-0328 §SD1). The
// host reads what the state manager already fetches each frame — the
// pointer, the wheel, zoom, the modifiers and the keys the shell watches —
// and counts a change of the active window. Typing with the pointer still is
// not seen: no fetch reports any key.

// inputSeen is what the last frame's input looked like.
type inputSeen struct {
	pointer   c.PointerValue
	modifiers c.ModifiersValue
	active    WindowKeyT
}

// SetInputListener installs f, told at each frame the person gave input;
// call it before the first Frame.
func (inst *Inst) SetInputListener(f func(now time.Time)) { inst.onInput = f }

// noteInput tells the listener when this frame shows input.
func (inst *Inst) noteInput(sm *c.StateManager, now time.Time) {
	if inst.onInput == nil || sm == nil {
		return
	}
	cur := inputSeen{pointer: sm.GetPointer(), modifiers: sm.GetModifiers(), active: inst.activeKey}
	scroll, zoom := sm.GetScrollDelta(), sm.GetZoomDelta()
	cmdEnter, _ := sm.GetCommandEnterPressed()
	if !cur.pointer.Valid || math.IsNaN(float64(cur.pointer.X)) || math.IsNaN(float64(cur.pointer.Y)) {
		// No pointer reads as NaN, which equals nothing, itself included.
		cur.pointer = c.PointerValue{}
	}
	moved := cur != inst.lastInput
	inst.lastInput = cur
	if moved || scroll.X != 0 || scroll.Y != 0 || (zoom.Zoom != 0 && zoom.Zoom != 1) ||
		sm.GetF1KeyPressed() || sm.GetF2KeyPressed() || sm.GetF8KeyPressed() || cmdEnter {
		inst.onInput(now)
	}
}

// ActiveWindow is the shell's active window, zero for none; render
// goroutine only, as host chrome is.
func (inst *Inst) ActiveWindow() uint64 { return uint64(inst.activeKey) }
