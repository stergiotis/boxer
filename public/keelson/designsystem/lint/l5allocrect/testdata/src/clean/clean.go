package clean

import "egui"

var c = egui.C

func topLevel() {
	// AllocateUiAtRect at top level (no enclosing flow) — perfectly fine.
	for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() {
	}
}

func insideFrame() {
	// Frame is not a flow container; AllocateUiAtRect inside is fine.
	for range c.Frame().KeepIter() {
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() {
		}
	}
}

func nonAllocateCall() {
	// Different selector — must not match.
	for range c.Vertical().KeepIter() {
		for range c.Horizontal().KeepIter() {
		}
	}
}
