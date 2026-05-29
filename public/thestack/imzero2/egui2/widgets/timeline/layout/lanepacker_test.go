//go:build llm_generated_opus47

package layout

import (
	"testing"
)

func TestPackLanes_EmptyInput(t *testing.T) {
	assn := PackLanes(nil)
	if assn.LaneCount() != 0 {
		t.Errorf("LaneCount: got %d want 0", assn.LaneCount())
	}
	if len(assn.EventLane) != 0 {
		t.Errorf("EventLane: got %d entries want 0", len(assn.EventLane))
	}
}

func TestPackLanes_AllNilEvents(t *testing.T) {
	assn := PackLanes([]*IntervalEvent{nil, nil, nil})
	if assn.LaneCount() != 0 {
		t.Errorf("LaneCount: got %d want 0", assn.LaneCount())
	}
}

func TestPackLanes_SingleEvent(t *testing.T) {
	ev := &IntervalEvent{FromMS: 0, ToMS: 100}
	assn := PackLanes([]*IntervalEvent{ev})
	if assn.LaneCount() != 1 {
		t.Fatalf("LaneCount: got %d want 1", assn.LaneCount())
	}
	if len(assn.Lanes[0].Items) != 1 || assn.Lanes[0].Items[0] != ev {
		t.Errorf("lane 0 items: got %v want [ev]", assn.Lanes[0].Items)
	}
	if assn.EventLane[ev] != 0 {
		t.Errorf("EventLane[ev]: got %d want 0", assn.EventLane[ev])
	}
}

func TestPackLanes_AllSequential_OneLane(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 10}
	b := &IntervalEvent{FromMS: 10, ToMS: 20}
	c := &IntervalEvent{FromMS: 20, ToMS: 30}
	assn := PackLanes([]*IntervalEvent{a, b, c})
	if assn.LaneCount() != 1 {
		t.Fatalf("LaneCount: got %d want 1", assn.LaneCount())
	}
	if len(assn.Lanes[0].Items) != 3 {
		t.Errorf("lane 0 items: got %d want 3", len(assn.Lanes[0].Items))
	}
}

func TestPackLanes_AllOverlapping_NLanes(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 100}
	b := &IntervalEvent{FromMS: 10, ToMS: 110}
	c := &IntervalEvent{FromMS: 20, ToMS: 120}
	assn := PackLanes([]*IntervalEvent{a, b, c})
	if assn.LaneCount() != 3 {
		t.Fatalf("LaneCount: got %d want 3", assn.LaneCount())
	}
	wants := map[*IntervalEvent]int32{a: 0, b: 1, c: 2}
	for ev, want := range wants {
		if got := assn.EventLane[ev]; got != want {
			t.Errorf("EventLane[%v]: got %d want %d", ev.FromMS, got, want)
		}
	}
}

func TestPackLanes_GreedyReusesEarliestFreedLane(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 100}
	b := &IntervalEvent{FromMS: 50, ToMS: 60}
	c := &IntervalEvent{FromMS: 100, ToMS: 200}
	assn := PackLanes([]*IntervalEvent{a, b, c})
	if assn.LaneCount() != 2 {
		t.Fatalf("LaneCount: got %d want 2", assn.LaneCount())
	}
	if assn.EventLane[a] != 0 {
		t.Errorf("a → lane %d want 0", assn.EventLane[a])
	}
	if assn.EventLane[b] != 1 {
		t.Errorf("b → lane %d want 1", assn.EventLane[b])
	}
	// Both lane 0 (a, freed at 100) and lane 1 (b, freed at 60) could host
	// c (FromMS=100). The earliest-freed-first greedy picks lane 1.
	if assn.EventLane[c] != 1 {
		t.Errorf("c → lane %d want 1 (lane 1 freed at 60 vs lane 0 freed at 100; earliest wins)", assn.EventLane[c])
	}
}

func TestPackLanes_TouchingBoundary_SameLaneAllowed(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 100}
	b := &IntervalEvent{FromMS: 100, ToMS: 200}
	assn := PackLanes([]*IntervalEvent{a, b})
	if assn.LaneCount() != 1 {
		t.Errorf("touching intervals should share a lane, got %d lanes", assn.LaneCount())
	}
}

func TestPackLanes_HintPinning_FirstSeenOrder(t *testing.T) {
	a := &IntervalEvent{FromMS: 100, ToMS: 110, LaneHint: "alice"}
	b := &IntervalEvent{FromMS: 0, ToMS: 10, LaneHint: "bob"}
	c := &IntervalEvent{FromMS: 50, ToMS: 60, LaneHint: "alice"}
	assn := PackLanes([]*IntervalEvent{a, b, c})
	if assn.LaneCount() != 2 {
		t.Fatalf("LaneCount: got %d want 2", assn.LaneCount())
	}
	if assn.Lanes[0].Hint != "alice" || assn.Lanes[1].Hint != "bob" {
		t.Errorf("hint order: got [%q,%q] want [alice,bob]", assn.Lanes[0].Hint, assn.Lanes[1].Hint)
	}
	if len(assn.Lanes[0].Items) != 2 {
		t.Errorf("alice items: got %d want 2", len(assn.Lanes[0].Items))
	}
	if assn.Lanes[0].Items[0] != c || assn.Lanes[0].Items[1] != a {
		t.Errorf("alice sort: got [%d,%d] want [50,100]", assn.Lanes[0].Items[0].FromMS, assn.Lanes[0].Items[1].FromMS)
	}
}

func TestPackLanes_HintAllowsOverlap(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 100, LaneHint: "shared"}
	b := &IntervalEvent{FromMS: 50, ToMS: 150, LaneHint: "shared"}
	assn := PackLanes([]*IntervalEvent{a, b})
	if assn.LaneCount() != 1 {
		t.Errorf("hint-pinned overlap should stay in one lane, got %d", assn.LaneCount())
	}
}

func TestPackLanes_MixedHintAndAuto_HintLanesFirst(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 100}
	b := &IntervalEvent{FromMS: 50, ToMS: 150, LaneHint: "pinned"}
	c := &IntervalEvent{FromMS: 200, ToMS: 300}
	assn := PackLanes([]*IntervalEvent{a, b, c})
	if assn.LaneCount() != 2 {
		t.Fatalf("LaneCount: got %d want 2", assn.LaneCount())
	}
	if assn.Lanes[0].Hint != "pinned" {
		t.Errorf("lane 0 should be the pinned lane, got hint %q", assn.Lanes[0].Hint)
	}
	if assn.EventLane[b] != 0 {
		t.Errorf("b → lane %d want 0 (hint first)", assn.EventLane[b])
	}
	if assn.EventLane[a] != 1 || assn.EventLane[c] != 1 {
		t.Errorf("auto-events: a→%d c→%d want both 1", assn.EventLane[a], assn.EventLane[c])
	}
}

func TestPackLanes_OutOfOrderInput_SortedByFromMS(t *testing.T) {
	c := &IntervalEvent{FromMS: 200, ToMS: 300}
	a := &IntervalEvent{FromMS: 0, ToMS: 100}
	b := &IntervalEvent{FromMS: 100, ToMS: 200}
	assn := PackLanes([]*IntervalEvent{c, a, b})
	if assn.LaneCount() != 1 {
		t.Errorf("sorted-then-packed should yield 1 lane, got %d", assn.LaneCount())
	}
	lane := assn.Lanes[0]
	if lane.Items[0] != a || lane.Items[1] != b || lane.Items[2] != c {
		t.Errorf("lane order after sort: got [%d,%d,%d] want [0,100,200]",
			lane.Items[0].FromMS, lane.Items[1].FromMS, lane.Items[2].FromMS)
	}
}

func TestPackLanes_InvertedIntervalsFilteredSilently(t *testing.T) {
	good := &IntervalEvent{FromMS: 0, ToMS: 100}
	inverted := &IntervalEvent{FromMS: 200, ToMS: 100}
	assn := PackLanes([]*IntervalEvent{good, inverted})
	if assn.LaneCount() != 1 {
		t.Errorf("LaneCount: got %d want 1 (inverted should be dropped)", assn.LaneCount())
	}
	if _, ok := assn.LaneOf(inverted); ok {
		t.Errorf("inverted event should not appear in EventLane")
	}
}

func TestIntervalEvent_Validate(t *testing.T) {
	if err := (IntervalEvent{FromMS: 0, ToMS: 100}.Validate()); err != nil {
		t.Errorf("good interval: got err %v want nil", err)
	}
	if err := (IntervalEvent{FromMS: 100, ToMS: 100}.Validate()); err != nil {
		t.Errorf("zero-width interval (FromMS == ToMS): got err %v want nil", err)
	}
	if err := (IntervalEvent{FromMS: 200, ToMS: 100}.Validate()); err == nil {
		t.Errorf("inverted interval: got nil want ErrIntervalInverted")
	}
}

func TestPackLanes_NilInterspersed_FilteredSilently(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 100}
	b := &IntervalEvent{FromMS: 200, ToMS: 300}
	assn := PackLanes([]*IntervalEvent{nil, a, nil, b, nil})
	if assn.LaneCount() != 1 {
		t.Errorf("LaneCount: got %d want 1", assn.LaneCount())
	}
	if len(assn.EventLane) != 2 {
		t.Errorf("EventLane entries: got %d want 2 (nils filtered)", len(assn.EventLane))
	}
}

func BenchmarkPackLanes_FullyOverlapping_10k(b *testing.B) {
	events := make([]*IntervalEvent, 10_000)
	for i := range events {
		events[i] = &IntervalEvent{FromMS: 0, ToMS: 1_000_000}
	}
	b.ResetTimer()
	for b.Loop() {
		_ = PackLanes(events)
	}
}

func BenchmarkPackLanes_ModestOverlap_10k(b *testing.B) {
	events := make([]*IntervalEvent, 10_000)
	for i := range events {
		offset := int64(i) * 100
		events[i] = &IntervalEvent{FromMS: offset, ToMS: offset + 1_000}
	}
	b.ResetTimer()
	for b.Loop() {
		_ = PackLanes(events)
	}
}

func TestPackLanes_EqualFromMS_StableInputOrder(t *testing.T) {
	a := &IntervalEvent{FromMS: 0, ToMS: 50}
	b := &IntervalEvent{FromMS: 0, ToMS: 50}
	c := &IntervalEvent{FromMS: 0, ToMS: 50}
	assn := PackLanes([]*IntervalEvent{a, b, c})
	if assn.LaneCount() != 3 {
		t.Fatalf("LaneCount: got %d want 3", assn.LaneCount())
	}
	if assn.EventLane[a] != 0 || assn.EventLane[b] != 1 || assn.EventLane[c] != 2 {
		t.Errorf("stable order: got [%d,%d,%d] want [0,1,2]",
			assn.EventLane[a], assn.EventLane[b], assn.EventLane[c])
	}
}
