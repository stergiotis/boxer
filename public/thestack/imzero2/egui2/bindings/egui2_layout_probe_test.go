package bindings

import (
	"testing"
)

// TestStackProbeSeqIsScopeDependent is the property ADR-0266 W7 asks for:
// the same scope key under two different parent scopes yields two slots,
// where the stack-free ProbeSeq yields one.
func TestStackProbeSeqIsScopeDependent(t *testing.T) {
	seqUnder := func(parent string) (seq uint64) {
		ids := NewWidgetIdStack()
		for range IdScope(ids.PrepareStr(parent)) {
			for range IdScope(ids.PrepareStr("chat")) {
				seq = ids.ProbeSeq("pane")
			}
		}
		return
	}
	a, b := seqUnder("left"), seqUnder("right")
	if a == b {
		t.Fatalf("same slot %#x under two parent scopes", a)
	}
	if a != seqUnder("left") {
		t.Fatalf("slot is not stable across frames: %#x vs %#x", a, seqUnder("left"))
	}
	if ProbeSeq("chat", "pane") != ProbeSeq("chat", "pane") {
		t.Fatal("stack-free ProbeSeq should be deterministic")
	}
}

func TestStackProbeSeqRolesAndChildIdsDiffer(t *testing.T) {
	ids := NewWidgetIdStack()
	for range IdScope(ids.PrepareStr("widget")) {
		pane, rect := ids.ProbeSeq("pane"), ids.ProbeSeq("rect")
		if pane == rect {
			t.Fatalf("two roles share slot %#x", pane)
		}
		if pane == 0 || rect == 0 {
			t.Fatal("a slot derived to zero")
		}
		// A child widget prepared from the role string must not take the slot.
		if child := ids.PrepareStr("pane").Derive(); child == pane {
			t.Fatalf("child id %#x equals the probe slot", child)
		}
	}
}

func TestStackProbeSeqIsSideEffectFree(t *testing.T) {
	ids := NewWidgetIdStack()
	for range IdScope(ids.PrepareStr("widget")) {
		before := ids.Depth()
		_ = ids.ProbeSeq("pane")
		if ids.Depth() != before {
			t.Fatal("ProbeSeq changed the stack depth")
		}
		// Callable between Prepare and Derive without breaking the state machine.
		prepared := ids.PrepareStr("child")
		_ = ids.ProbeSeq("pane")
		if prepared.Derive() == 0 {
			t.Fatal("prepared id lost")
		}
		// And a fresh Prepare afterwards must not panic on a stale state.
		_ = ids.PrepareStr("next").Derive()
	}
}
