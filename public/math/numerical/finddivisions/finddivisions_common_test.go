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
