package finddivisions

import (
	"math"
	"testing"
)

// GenerateTicksRobust must never spin on a degenerate (start,end,step): a step
// tiny relative to the span — which the Talbot search probes at extreme
// magnitudes, where a near-zero-width span sits near 2^63 — makes
// (end-start)/step explode to ~1e14+. The loop is capped instead of running
// that many iterations, and a non-finite step yields no ticks (the caller falls
// back to a simpler axis). A normal range is unaffected.
func TestGenerateTicksRobustBoundsCount(t *testing.T) {
	// A ~1e-9 step over a 1e6 span is 1e15 ticks unbounded; must be capped.
	if got := GenerateTicksRobust(0, 1e6, 1e-9); len(got) > 10001 {
		t.Fatalf("unbounded tick count: %d (want <= maxTicks+1)", len(got))
	}
	// The extreme-magnitude near-zero-width case behind the World-map hang.
	if got := GenerateTicksRobust(1.8e19, 1.8e19+20000, 1e-3); len(got) > 10001 {
		t.Fatalf("extreme-magnitude tick count not bounded: %d", len(got))
	}
	// Non-finite / zero step → no ticks.
	if ticks := GenerateTicksRobust(0, 1, 0); len(ticks) != 0 {
		t.Fatalf("zero step must yield no ticks, got %d", len(ticks))
	}
	if ticks := GenerateTicksRobust(0, math.Inf(1), 1); len(ticks) != 0 {
		t.Fatalf("infinite range must yield no ticks, got %d", len(ticks))
	}
	// A normal range is untouched: 0,2,4,6,8,10.
	if ticks := GenerateTicksRobust(0, 10, 2); len(ticks) != 6 {
		t.Fatalf("normal range: got %d ticks, want 6", len(ticks))
	}
}

// GenerateTicksRobust snaps away the float noise left by start+i*step:
// 3*0.4 is 1.2000000000000002, and a shortest-round-trip formatter prints
// every one of those digits onto the axis. The comparisons below are exact
// on purpose — a tolerance would pass on the values this test exists to
// reject.
func TestGenerateTicksRobustSnapsFloatNoise(t *testing.T) {
	got := GenerateTicksRobust(0, 1.6, 0.4)
	want := []float64{0, 0.4, 0.8, 1.2, 1.6}
	if len(got) != len(want) {
		t.Fatalf("got %d ticks %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tick %d: got %v, want %v (exactly)", i, got[i], want[i])
		}
	}

	// The grid sits ten decades below the step, so a fine step keeps every
	// digit it needs.
	fine := GenerateTicksRobust(0, 0.0003, 0.0001)
	for i, want := range []float64{0, 0.0001, 0.0002, 0.0003} {
		if i < len(fine) && fine[i] != want {
			t.Errorf("fine tick %d: got %v, want %v (exactly)", i, fine[i], want)
		}
	}

	// A step that is not a decimal at all is still reproduced to well past
	// display precision — snapping erases noise, not data.
	third := GenerateTicksRobust(0, 1, 1.0/3.0)
	if len(third) < 4 {
		t.Fatalf("got %d ticks for a 1/3 step, want 4", len(third))
	}
	if d := math.Abs(third[3] - 1.0); d > 1e-10 {
		t.Errorf("1/3 step: tick 3 is %v, off by %v", third[3], d)
	}
}
