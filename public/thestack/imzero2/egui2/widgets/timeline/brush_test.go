package timeline

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// The brush gesture is a state machine over frames, because its input arrives
// a frame late (ADR-0043 §SD16). These drive it frame by frame the way
// renderBrushStrip does, which is the only way to test it without a renderer.

const (
	brushViewMinMS int64 = 1_700_000_000_000
	brushViewMaxMS int64 = 1_700_000_060_000 // +60 s
	brushAxisStart       = 100.0
	brushAxisEnd         = 1_100.0 // 1000 px for 60 s → 60 ms per px
)

func brushTestMap() layout.TickMap {
	return layout.ComputeTickMap(
		time.UnixMilli(brushViewMinMS).UTC(), time.UnixMilli(brushViewMaxMS).UTC(),
		brushAxisStart, brushAxisEnd, nil, timeticks.TimeStep{})
}

func brushTestLayout() verticalLayout {
	return verticalLayout{axisStartPx: brushAxisStart, axisEndPx: brushAxisEnd}
}

// brushFixture builds a brush-enabled timeline plus a recorder for what the
// listener saw.
func brushFixture(t *testing.T) (inst *Timeline, got *[]BrushRange, seen *[]bool) {
	t.Helper()
	ranges := make([]BrushRange, 0, 4)
	flags := make([]bool, 0, 4)
	inst = New(c.NewWidgetIdStack(), "brush-test", nil,
		WithContainerWidth(1200),
		WithBrush(func(r BrushRange, ok bool) {
			ranges = append(ranges, r)
			flags = append(flags, ok)
		}))
	return inst, &ranges, &flags
}

// drive runs one frame of the gesture machine.
func (inst *Timeline) driveBrush(down bool, x float32) {
	_ = inst.advanceBrush(brushTestMap(), down, x, true, brushViewMinMS, brushViewMaxMS)
}

func TestBrush_DisabledByDefault(t *testing.T) {
	inst := New(c.NewWidgetIdStack(), "no-brush", nil, WithContainerWidth(1200))
	if inst.brushReserved() {
		t.Fatal("a timeline without WithBrush must reserve no brush strip")
	}
	if _, ok := inst.Brush(); ok {
		t.Fatal("nothing should be brushed before any gesture")
	}
}

func TestBrush_EnabledByOption(t *testing.T) {
	inst, _, _ := brushFixture(t)
	if !inst.brushReserved() {
		t.Fatal("WithBrush must enable the strip")
	}
}

// TestBrush_DragCommitsRange is the happy path: press, travel, release.
func TestBrush_DragCommitsRange(t *testing.T) {
	inst, got, oks := brushFixture(t)

	inst.driveBrush(true, 200)  // press at +100 px → +6 s
	inst.driveBrush(true, 400)  // drag to +300 px → +18 s
	inst.driveBrush(false, 400) // release

	r, ok := inst.Brush()
	if !ok {
		t.Fatal("a completed drag must commit a range")
	}
	wantFrom := brushViewMinMS + 6_000
	wantTo := brushViewMinMS + 18_000
	if r.FromMS != wantFrom || r.ToMS != wantTo {
		t.Errorf("range: got [%d,%d] want [%d,%d]", r.FromMS, r.ToMS, wantFrom, wantTo)
	}
	if len(*got) != 1 || !(*oks)[0] {
		t.Fatalf("listener: got %d calls %v, want one ok call", len(*got), *oks)
	}
	if (*got)[0] != r {
		t.Errorf("listener saw %+v, Brush() reports %+v", (*got)[0], r)
	}
}

// TestBrush_RightToLeftNormalises pins that the gesture has no direction: the
// range is the interval, not the order the ends were placed in.
func TestBrush_RightToLeftNormalises(t *testing.T) {
	inst, _, _ := brushFixture(t)

	inst.driveBrush(true, 600)
	inst.driveBrush(true, 300)
	inst.driveBrush(false, 300)

	r, ok := inst.Brush()
	if !ok {
		t.Fatal("a right-to-left drag must still commit")
	}
	if r.FromMS >= r.ToMS {
		t.Errorf("range not normalised: [%d,%d]", r.FromMS, r.ToMS)
	}
	if r.FromMS != brushViewMinMS+12_000 || r.ToMS != brushViewMinMS+30_000 {
		t.Errorf("range: got [%d,%d]", r.FromMS-brushViewMinMS, r.ToMS-brushViewMinMS)
	}
}

// TestBrush_ClickClears pins the gesture that travelled nothing: it is a
// clear, not an empty selection, because an empty range maps to an empty
// replay window and reads as a broken control.
func TestBrush_ClickClears(t *testing.T) {
	inst, got, oks := brushFixture(t)

	inst.SetBrush(brushViewMinMS+1_000, brushViewMinMS+2_000)
	if _, ok := inst.Brush(); !ok {
		t.Fatal("SetBrush should have committed a range")
	}

	inst.driveBrush(true, 500)
	inst.driveBrush(false, 500) // release without travel

	if _, ok := inst.Brush(); ok {
		t.Error("a click must clear the brush")
	}
	if len(*got) != 1 || (*oks)[0] {
		t.Fatalf("listener: got %v, want one not-ok call", *oks)
	}
}

// TestBrush_SubThresholdTravelClears covers a shaky click rather than a
// deliberate drag.
func TestBrush_SubThresholdTravelClears(t *testing.T) {
	inst, _, oks := brushFixture(t)

	inst.driveBrush(true, 500)
	inst.driveBrush(true, 501) // 1 px, under brushMinDragPx
	inst.driveBrush(false, 501)

	if _, ok := inst.Brush(); ok {
		t.Error("travel under the threshold must not commit")
	}
	if len(*oks) != 1 || (*oks)[0] {
		t.Fatalf("listener: got %v, want one not-ok call", *oks)
	}
}

// TestBrush_ClampsToView pins that a drag past either end selects up to the
// edge rather than beyond it — the view bounds are the only times that exist
// on screen.
func TestBrush_ClampsToView(t *testing.T) {
	inst, _, _ := brushFixture(t)

	_ = inst.advanceBrush(brushTestMap(), true, brushAxisStart-500, true, brushViewMinMS, brushViewMaxMS)
	_ = inst.advanceBrush(brushTestMap(), true, brushAxisEnd+500, true, brushViewMinMS, brushViewMaxMS)
	_ = inst.advanceBrush(brushTestMap(), false, brushAxisEnd+500, true, brushViewMinMS, brushViewMaxMS)

	r, ok := inst.Brush()
	if !ok {
		t.Fatal("an over-wide drag must still commit")
	}
	if r.FromMS < brushViewMinMS || r.ToMS > brushViewMaxMS {
		t.Errorf("range escaped the view: [%d,%d] outside [%d,%d]",
			r.FromMS, r.ToMS, brushViewMinMS, brushViewMaxMS)
	}
}

// TestBrush_InFlightDoesNotDisturbTheCommitted pins the reason anchor/cur and
// from/to are separate fields: starting a new gesture must not destroy the
// range the user already has until the new one finishes.
func TestBrush_InFlightDoesNotDisturbTheCommitted(t *testing.T) {
	inst, _, _ := brushFixture(t)

	inst.driveBrush(true, 200)
	inst.driveBrush(true, 400)
	inst.driveBrush(false, 400)
	first, _ := inst.Brush()

	inst.driveBrush(true, 700) // a new gesture begins
	inst.driveBrush(true, 900)
	mid, ok := inst.Brush()
	if !ok || mid != first {
		t.Errorf("committed range changed mid-gesture: %+v -> %+v", first, mid)
	}

	inst.driveBrush(false, 900)
	final, _ := inst.Brush()
	if final == first {
		t.Error("the finished gesture should have replaced the range")
	}
}

// TestBrush_MissingCursorHoldsTheGesture covers a drag that leaves the strip:
// the per-canvas pointer reports nothing, and dropping the sample would freeze
// the pending range instead of tracking to the edge.
func TestBrush_MissingCursorHoldsTheGesture(t *testing.T) {
	inst, _, _ := brushFixture(t)

	_ = inst.advanceBrush(brushTestMap(), true, 300, true, brushViewMinMS, brushViewMaxMS)
	_ = inst.advanceBrush(brushTestMap(), true, 600, true, brushViewMinMS, brushViewMaxMS)
	// Pointer left the strip: no usable x this frame.
	_ = inst.advanceBrush(brushTestMap(), true, 0, false, brushViewMinMS, brushViewMaxMS)
	if !inst.brushing {
		t.Fatal("losing the cursor must not abort the gesture")
	}
	if inst.brushCurMS != brushViewMinMS+30_000 {
		t.Errorf("pending end moved on a sample-less frame: %d", inst.brushCurMS-brushViewMinMS)
	}

	_ = inst.advanceBrush(brushTestMap(), false, 0, false, brushViewMinMS, brushViewMaxMS)
	if _, ok := inst.Brush(); !ok {
		t.Error("the gesture should still commit what it had")
	}
}

// TestBrush_PressWithoutCursorIsIgnored pins the mirror case: a press whose
// position is unknown cannot seed an anchor.
func TestBrush_PressWithoutCursorIsIgnored(t *testing.T) {
	inst, _, _ := brushFixture(t)
	_ = inst.advanceBrush(brushTestMap(), true, 0, false, brushViewMinMS, brushViewMaxMS)
	if inst.brushing {
		t.Error("a press with no cursor must not start a gesture")
	}
}

func TestBrush_SetAndClear(t *testing.T) {
	inst, got, _ := brushFixture(t)

	inst.SetBrush(2_000, 1_000) // inverted
	r, ok := inst.Brush()
	if !ok || r.FromMS != 1_000 || r.ToMS != 2_000 {
		t.Errorf("SetBrush must normalise: got %+v ok=%v", r, ok)
	}

	inst.SetBrush(5_000, 5_000) // empty
	if _, ok := inst.Brush(); ok {
		t.Error("an empty range must clear rather than commit")
	}

	inst.SetBrush(1, 2)
	inst.ClearBrush()
	if _, ok := inst.Brush(); ok {
		t.Error("ClearBrush must clear")
	}
	if len(*got) != 0 {
		t.Errorf("programmatic changes must not fire the listener; got %d calls", len(*got))
	}
}

// TestBrush_NilListenerIsSafe pins the documented nil-is-a-no-op tier.
func TestBrush_NilListenerIsSafe(t *testing.T) {
	inst := New(c.NewWidgetIdStack(), "brush-nil", nil,
		WithContainerWidth(1200), WithBrush(nil))

	inst.driveBrush(true, 200)
	inst.driveBrush(true, 400)
	inst.driveBrush(false, 400)

	if _, ok := inst.Brush(); !ok {
		t.Error("a nil listener must not stop the brush tracking")
	}
}

// TestBrush_PaintIsInertOutsideTheView guards the paint path's early exits
// against a range that cannot be seen — it must draw nothing rather than
// clamp to a misleading edge-to-edge bar.
func TestBrush_PaintIsInertOutsideTheView(t *testing.T) {
	inst, _, _ := brushFixture(t)
	vl := brushTestLayout()

	inst.SetBrush(brushViewMinMS-10_000, brushViewMinMS-5_000)
	inst.paintBrushStrip(brushTestMap(), vl, brushViewMinMS, brushViewMaxMS)

	inst.SetBrush(brushViewMaxMS+5_000, brushViewMaxMS+10_000)
	inst.paintBrushStrip(brushTestMap(), vl, brushViewMinMS, brushViewMaxMS)

	inst.ClearBrush()
	inst.paintBrushStrip(brushTestMap(), vl, brushViewMinMS, brushViewMaxMS)
}

// TestBrush_SettledReportsGestureEnd pins the flag renderBrushStrip uses to
// tell a click it already handled from one the press-sampling never saw. A
// fast click — every synthesised one — presses and releases inside a frame, so
// the machine observes nothing and the widget must fall back to egui's click
// edge. Without the flag that fallback would fire a second time on every
// gesture the machine did handle.
func TestBrush_SettledReportsGestureEnd(t *testing.T) {
	inst, _, _ := brushFixture(t)
	tm := brushTestMap()

	if settled := inst.advanceBrush(tm, true, 200, true, brushViewMinMS, brushViewMaxMS); settled {
		t.Error("a press does not settle a gesture")
	}
	if settled := inst.advanceBrush(tm, true, 400, true, brushViewMinMS, brushViewMaxMS); settled {
		t.Error("a drag does not settle a gesture")
	}
	if settled := inst.advanceBrush(tm, false, 400, true, brushViewMinMS, brushViewMaxMS); !settled {
		t.Error("a release settles the gesture")
	}
	if settled := inst.advanceBrush(tm, false, 400, true, brushViewMinMS, brushViewMaxMS); settled {
		t.Error("an idle frame settles nothing")
	}
}

// TestBrush_ClearAndNotifyFiresOnce pins that both routes to "no range" tell
// the listener exactly once.
func TestBrush_ClearAndNotifyFiresOnce(t *testing.T) {
	inst, got, oks := brushFixture(t)
	inst.SetBrush(brushViewMinMS+1_000, brushViewMinMS+2_000)

	inst.clearBrushAndNotify()

	if _, ok := inst.Brush(); ok {
		t.Error("the range should be gone")
	}
	if len(*got) != 1 || (*oks)[0] {
		t.Fatalf("listener: got %v, want exactly one not-ok call", *oks)
	}
}
