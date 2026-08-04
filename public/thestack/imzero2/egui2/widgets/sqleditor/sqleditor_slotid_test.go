package sqleditor

import "testing"

// TestSlotIdIsPerEditor guards the editor's register slots (the r21 pane probe
// and the r9 row-height measure) against being shared. IDSlot separates two
// editors inside one app, which is what it was introduced for; it cannot
// separate two windows of the same app, because the embedder passes a constant
// ("sqlEditor" in play). Sharing means each editor sizes itself from the
// other's pane — the r18 failure, reproduced inside the seq-keyed register.
func TestSlotIdIsPerEditor(t *testing.T) {
	a, b := New(), New()
	if a.slotId("sqlEditor", "pane") == b.slotId("sqlEditor", "pane") {
		t.Fatal("two editors share one pane slot")
	}
	if a.slotId("sqlEditor", "pane") == a.slotId("sqlEditor", "row-h") {
		t.Fatal("two roles of one editor share a slot")
	}
	if first, again := a.slotId("sqlEditor", "pane"), a.slotId("sqlEditor", "pane"); first != again {
		t.Fatalf("slot moved between calls: %#016x then %#016x", first, again)
	}

	// The zero value is a documented construction, so it has to mint a salt on
	// first use rather than fall through to the unsalted seq.
	var z1, z2 Editor
	if z1.slotId("sqlEditor", "pane") == z2.slotId("sqlEditor", "pane") {
		t.Fatal("two zero-value editors share one pane slot")
	}
}
