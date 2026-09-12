package bindings

import "github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"

// Scripted input. The setters below place a value in one of the per-frame
// registers exactly as Sync does when it drains the host at frame end, so a
// headless scene or a widget test can play one frame of input into a
// widget's Render without a client: script, render, read the widget's
// events, repeat. The one-frame lag of the live path holds by construction
// — a scripted value is what the widget sees on its next Render. A live
// host overwrites every register at its next Sync, so a script and a
// client do not mix within one frame. ScriptReset clears what a script
// wrote; nothing else is cleared between frames, so a script that wants a
// register empty says so.

// ScriptResponse sets the response flags the widget behind the handle
// reports on the next read.
func (inst *StateManager) ScriptResponse(h widgethandle.WidgetHandle, flags ResponseFlagsE) {
	inst.responseFlags.UpsertSingle(h.Resolve(), flags)
}

// ScriptCanvasCursor sets a paintCanvas's R24 pointer row: its screen origin
// and the canvas-relative pointer, NaN when the pointer is not over it.
func (inst *StateManager) ScriptCanvasCursor(h widgethandle.WidgetHandle, v CanvasCursorValue) {
	inst.r24CanvasPointers[h.Resolve()] = v
}

// ScriptCanvasWheel sets a paintCanvas's R23 wheel capture; an absent row
// reads as the identity, so a script sets it for the gesture's frame and
// clears it after.
func (inst *StateManager) ScriptCanvasWheel(h widgethandle.WidgetHandle, v CanvasWheelValue) {
	inst.r23CanvasWheel[h.Resolve()] = v
}

// ScriptPointer sets the R20 global pointer.
func (inst *StateManager) ScriptPointer(v PointerValue) { inst.r20Pointer = v }

// ScriptModifiers sets the R17 modifier state.
func (inst *StateManager) ScriptModifiers(v ModifiersValue) { inst.r17Modifiers = v }

// ScriptReset is the scripted frame boundary: it clears the registers the
// Script* setters fill — every response, canvas row and wheel capture, the
// pointer and the modifiers — and the frame's seen-id set, as Sync does.
func (inst *StateManager) ScriptReset() {
	seenIds.Clear()
	inst.responseFlags.Reset()
	clear(inst.r24CanvasPointers)
	clear(inst.r23CanvasWheel)
	inst.r20Pointer = PointerValue{}
	inst.r17Modifiers = ModifiersValue{}
}
