package timeline

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The playhead is the caller-set instant marker. Its state is a plain
// (value, present) pair, and the only behaviour worth pinning is that it
// distinguishes "set to zero" from "not set" — epoch millisecond zero is a
// real time, so a sentinel would have made 1970 unmarkable.

func playheadFixture() (inst *Timeline) {
	inst = New(c.NewWidgetIdStack(), "playhead-test", nil, WithContainerWidth(1200))
	return
}

func TestPlayhead_UnsetByDefault(t *testing.T) {
	inst := playheadFixture()
	if _, ok := inst.Playhead(); ok {
		t.Fatalf("Playhead() reported a mark on a fresh timeline")
	}
}

func TestPlayheadRoundTrips(t *testing.T) {
	inst := playheadFixture()
	inst.SetPlayhead(1_700_000_000_000)
	tMS, ok := inst.Playhead()
	if !ok {
		t.Fatalf("Playhead() = not set, want set")
	}
	if tMS != 1_700_000_000_000 {
		t.Fatalf("Playhead() = %d, want 1700000000000", tMS)
	}
}

// TestPlayheadAtEpochZeroIsStillSet is why presence has its own flag.
func TestPlayheadAtEpochZeroIsStillSet(t *testing.T) {
	inst := playheadFixture()
	inst.SetPlayhead(0)
	tMS, ok := inst.Playhead()
	if !ok || tMS != 0 {
		t.Fatalf("Playhead() = (%d, %v), want (0, true)", tMS, ok)
	}
}

func TestClearPlayhead(t *testing.T) {
	inst := playheadFixture()
	inst.SetPlayhead(1_700_000_000_000)
	inst.ClearPlayhead()
	if _, ok := inst.Playhead(); ok {
		t.Fatalf("Playhead() still reported a mark after ClearPlayhead")
	}
}

// TestPlayheadIsIndependentOfTheNowLine pins that the two marks do not share
// state: a caller that turns the now line off still gets its playhead, which
// is exactly the case a historical view is in.
func TestPlayheadIsIndependentOfTheNowLine(t *testing.T) {
	inst := playheadFixture()
	inst.SetPlayhead(1_700_000_000_000)
	inst.SetNowLine(false)
	if _, ok := inst.Playhead(); !ok {
		t.Fatalf("turning the now line off cleared the playhead")
	}
	inst.SetNowLine(true)
	inst.ClearPlayhead()
	if !inst.nowLineEnabled {
		t.Fatalf("clearing the playhead turned the now line off")
	}
}
