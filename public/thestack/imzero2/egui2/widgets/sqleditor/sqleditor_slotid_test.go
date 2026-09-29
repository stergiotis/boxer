package sqleditor

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// TestSlotIdIsPerEditor guards the editor's register slots (the r21 pane probe
// and the r9 row-height measure) against being shared: the slot is derived
// from the id stack under the editor's own scope (ADR-0267 W7), so two editors
// under one stack differ by scope key, and two windows of one app — whose
// host pushes a per-window scope around Frame — differ by that scope.
func TestSlotIdIsPerEditor(t *testing.T) {
	ids := c.NewWidgetIdStack()
	a, b := New(ids, "a"), New(ids, "b")
	slot := func(e *Editor, role string) (seq uint64) {
		for range c.IdScope(ids.PrepareStr(e.scopeKey)) {
			seq = e.slotId(role)
		}
		return
	}
	if slot(a, "pane") == slot(b, "pane") {
		t.Fatal("two editors share one pane slot")
	}
	if slot(a, "pane") == slot(a, "row-h") {
		t.Fatal("two roles of one editor share a slot")
	}
	if first, again := slot(a, "pane"), slot(a, "pane"); first != again {
		t.Fatalf("slot moved between calls: %#016x then %#016x", first, again)
	}
	// The same scope key under two host scopes — two windows of one app.
	var w1, w2 uint64
	for range c.IdScope(ids.PrepareStr("window-1")) {
		w1 = slot(a, "pane")
	}
	for range c.IdScope(ids.PrepareStr("window-2")) {
		w2 = slot(a, "pane")
	}
	if w1 == w2 {
		t.Fatal("two windows share one pane slot")
	}
}
