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

// The gesture machine reads egui's own response flags rather than
// reconstructing edges from the button-down bit, so a driven frame is the flag
// set egui would deliver for that phase of a gesture. egui sets `clicked` or
// `drag_stopped` on a release, never both — which is why there is no frame
// here that carries the two together outside the test that pins the ordering.
const (
	brushFramePress   = c.IsPointerButtonDownResponseFlags
	brushFrameDrag    = c.IsPointerButtonDownResponseFlags | c.DraggedResponseFlags
	brushFrameRelease = c.DragStoppedResponseFlags
	brushFrameClick   = c.PrimaryClickedResponseFlags
	brushFrameIdle    = c.NilResponseFlags
)

// driveBrush runs one frame of the gesture machine with a usable cursor.
func (inst *Timeline) driveBrush(flags c.ResponseFlagsE, x float32) {
	inst.advanceBrush(brushTestMap(), flags, x, true, brushViewMinMS, brushViewMaxMS)
}

// driveBrushBlind runs one frame with no usable cursor position — the pointer
// left the strip, or none has been seen.
func (inst *Timeline) driveBrushBlind(flags c.ResponseFlagsE) {
	inst.advanceBrush(brushTestMap(), flags, 0, false, brushViewMinMS, brushViewMaxMS)
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

	inst.driveBrush(brushFramePress, 200) // press at +100 px → +6 s
	inst.driveBrush(brushFrameDrag, 400)  // drag to +300 px → +18 s
	inst.driveBrush(brushFrameRelease, 400)

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

	inst.driveBrush(brushFramePress, 600)
	inst.driveBrush(brushFrameDrag, 300)
	inst.driveBrush(brushFrameRelease, 300)

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

	inst.driveBrush(brushFramePress, 500)
	inst.driveBrush(brushFrameClick, 500) // egui called the release a click

	if _, ok := inst.Brush(); ok {
		t.Error("a click must clear the brush")
	}
	if len(*got) != 1 || (*oks)[0] {
		t.Fatalf("listener: got %v, want one not-ok call", *oks)
	}
}

// TestBrush_NoLocalTravelThreshold pins the one behaviour that moved when the
// machine started reading egui's edges: click-vs-drag is egui's call, and the
// widget no longer second-guesses it with a pixel count of its own. A gesture
// egui reports as a drag commits however short it was — and the shaky clicks
// the old threshold existed to absorb now arrive as `clicked` instead, which
// clears (see TestBrush_ClickClears).
func TestBrush_NoLocalTravelThreshold(t *testing.T) {
	inst, _, oks := brushFixture(t)

	inst.driveBrush(brushFramePress, 500)
	inst.driveBrush(brushFrameDrag, 501) // 1 px — egui still called it a drag
	inst.driveBrush(brushFrameRelease, 501)

	r, ok := inst.Brush()
	if !ok {
		t.Fatal("a gesture egui reports as a drag must commit")
	}
	// 1 px is 60 ms on this fixture's axis, which is a real range.
	if r.ToMS-r.FromMS != 60 {
		t.Errorf("range: got %d ms, want 60", r.ToMS-r.FromMS)
	}
	if len(*oks) != 1 || !(*oks)[0] {
		t.Fatalf("listener: got %v, want one ok call", *oks)
	}
}

// TestBrush_ZeroTravelDragClears covers the drag egui reports for a press held
// still past its click timeout: real by egui's reckoning, but it describes no
// range, so it clears rather than committing an empty one that would map to an
// empty replay window.
func TestBrush_ZeroTravelDragClears(t *testing.T) {
	inst, _, oks := brushFixture(t)
	inst.SetBrush(brushViewMinMS+1_000, brushViewMinMS+2_000)

	inst.driveBrush(brushFramePress, 500)
	inst.driveBrush(brushFrameDrag, 500)
	inst.driveBrush(brushFrameRelease, 500)

	if _, ok := inst.Brush(); ok {
		t.Error("a drag that described no range must clear")
	}
	if len(*oks) != 1 || (*oks)[0] {
		t.Fatalf("listener: got %v, want one not-ok call", *oks)
	}
}

// TestBrush_FastClickClears is the case that used to need a special path: a
// press and release inside one frame — every synthesised click, and a quick
// human one — is never seen as button-down at all, so the machine observes
// nothing but egui's click edge. It now takes the same branch a slow click
// does.
func TestBrush_FastClickClears(t *testing.T) {
	inst, _, oks := brushFixture(t)
	inst.SetBrush(brushViewMinMS+1_000, brushViewMinMS+2_000)

	inst.driveBrush(brushFrameClick, 500)

	if _, ok := inst.Brush(); ok {
		t.Error("a click the machine never saw press must still clear")
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

	inst.driveBrush(brushFramePress, brushAxisStart-500)
	inst.driveBrush(brushFrameDrag, brushAxisEnd+500)
	inst.driveBrush(brushFrameRelease, brushAxisEnd+500)

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

	inst.driveBrush(brushFramePress, 200)
	inst.driveBrush(brushFrameDrag, 400)
	inst.driveBrush(brushFrameRelease, 400)
	first, _ := inst.Brush()

	inst.driveBrush(brushFramePress, 700) // a new gesture begins
	inst.driveBrush(brushFrameDrag, 900)
	mid, ok := inst.Brush()
	if !ok || mid != first {
		t.Errorf("committed range changed mid-gesture: %+v -> %+v", first, mid)
	}

	inst.driveBrush(brushFrameRelease, 900)
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

	inst.driveBrush(brushFramePress, 300)
	inst.driveBrush(brushFrameDrag, 600)
	// Pointer left the strip: no usable x this frame.
	inst.driveBrushBlind(brushFrameDrag)
	if !inst.brushing {
		t.Fatal("losing the cursor must not abort the gesture")
	}
	if inst.brushCurMS != brushViewMinMS+30_000 {
		t.Errorf("pending end moved on a sample-less frame: %d", inst.brushCurMS-brushViewMinMS)
	}

	inst.driveBrushBlind(brushFrameRelease)
	if _, ok := inst.Brush(); !ok {
		t.Error("the gesture should still commit what it had")
	}
}

// TestBrush_PressWithoutCursorIsIgnored pins the mirror case: a press whose
// position is unknown cannot seed an anchor.
func TestBrush_PressWithoutCursorIsIgnored(t *testing.T) {
	inst, _, _ := brushFixture(t)
	inst.driveBrushBlind(brushFramePress)
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

	inst.driveBrush(brushFramePress, 200)
	inst.driveBrush(brushFrameDrag, 400)
	inst.driveBrush(brushFrameRelease, 400)

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

// TestBrush_OneGestureEndsOnce is what the retired `settled` flag used to
// guarantee by hand: a gesture tells the listener once, whatever the ending
// frame carries. egui never sets `clicked` and `drag_stopped` together, so the
// frame below is not one it would send — the point is that the switch resolves
// it to a single branch rather than firing both, and that a run of idle frames
// after an ending adds nothing.
func TestBrush_OneGestureEndsOnce(t *testing.T) {
	inst, _, oks := brushFixture(t)

	inst.driveBrush(brushFramePress, 200)
	inst.driveBrush(brushFrameDrag, 400)
	inst.driveBrush(brushFrameClick|brushFrameRelease, 400)
	inst.driveBrush(brushFrameIdle, 400)
	inst.driveBrush(brushFrameIdle, 400)

	if len(*oks) != 1 {
		t.Fatalf("listener: got %d calls %v, want exactly one", len(*oks), *oks)
	}
	if inst.brushing {
		t.Error("the gesture must be over")
	}
}

// TestBrush_LostEndingStillEndsTheGesture drives the safety net: a gesture
// whose ending edge never arrives (the button is simply no longer down on the
// strip) must settle anyway, or the pending fill paints for the rest of the
// session.
func TestBrush_LostEndingStillEndsTheGesture(t *testing.T) {
	inst, _, oks := brushFixture(t)

	inst.driveBrush(brushFramePress, 200)
	inst.driveBrush(brushFrameDrag, 400)
	inst.driveBrush(brushFrameIdle, 400) // no click, no drag_stopped

	if inst.brushing {
		t.Fatal("a gesture with no ending edge must not stay in flight")
	}
	if _, ok := inst.Brush(); !ok {
		t.Error("what the gesture had should still commit")
	}
	if len(*oks) != 1 || !(*oks)[0] {
		t.Fatalf("listener: got %v, want one ok call", *oks)
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

// The resting affordance (§SD19). Only its layout is testable without a
// renderer, which is the part that decides whether the caption is shown at
// all — the paint calls it feeds are a fixed sequence over these numbers.

func hintLayout(t *testing.T, axisStart, axisEnd, stripH float32, text string) (l brushHintLayout, ok bool) {
	t.Helper()
	return computeBrushHintLayout(
		verticalLayout{axisStartPx: axisStart, axisEndPx: axisEnd}, stripH, text)
}

func TestBrushHint_RailSpansAxisAndCapsStayInBounds(t *testing.T) {
	l, ok := hintLayout(t, brushAxisStart, brushAxisEnd, 14, defaultBrushHintText)
	if !ok {
		t.Fatal("a strip with an axis must produce a hint layout")
	}
	if l.x0 != brushAxisStart || l.x1 != brushAxisEnd {
		t.Fatalf("rail must span the axis, got [%v,%v]", l.x0, l.x1)
	}
	if l.railY != 7 {
		t.Fatalf("rail must sit at mid-height, got %v", l.railY)
	}
	// axisEndPx is the canvas width exactly; a cap drawn on it would lose half
	// its stroke off the right edge.
	if l.capX0 <= l.x0 || l.capX1 >= l.x1 {
		t.Fatalf("caps must be inset from the canvas bounds, got %v / %v", l.capX0, l.capX1)
	}
	if l.capH > 14 {
		t.Fatalf("cap must fit the strip, got %v", l.capH)
	}
}

func TestBrushHint_CaptionCentredWithClearRailEitherSide(t *testing.T) {
	l, ok := hintLayout(t, brushAxisStart, brushAxisEnd, 14, defaultBrushHintText)
	if !ok {
		t.Fatal("expected a layout")
	}
	if l.text != defaultBrushHintText {
		t.Fatalf("a wide strip must carry the caption, got %q", l.text)
	}
	mid := float32(brushAxisStart+brushAxisEnd) / 2
	if l.textX != mid {
		t.Fatalf("caption must be centred on the axis, got %v want %v", l.textX, mid)
	}
	if !(l.gapX0 < mid && mid < l.gapX1) {
		t.Fatalf("rail break must straddle the caption, got [%v,%v]", l.gapX0, l.gapX1)
	}
	if l.gapX0-l.x0 < brushHintMinFreePx || l.x1-l.gapX1 < brushHintMinFreePx {
		t.Fatalf("rail stubs too short: [%v,%v] in [%v,%v]", l.gapX0, l.gapX1, l.x0, l.x1)
	}
}

// The caption is the part that yields; the rail is not. Each of these is a way
// the space for it can run out, and none of them may leave a half-drawn track.
func TestBrushHint_CaptionDropsButRailSurvives(t *testing.T) {
	for _, tc := range []struct {
		name            string
		start, end, hgt float32
		text            string
	}{
		{"axis too narrow", 100, 240, 14, defaultBrushHintText},
		{"strip too short", brushAxisStart, brushAxisEnd, brushHintMinStripH - 1, defaultBrushHintText},
		{"caller cleared the text", brushAxisStart, brushAxisEnd, 14, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, ok := hintLayout(t, tc.start, tc.end, tc.hgt, tc.text)
			if !ok {
				t.Fatal("the rail must still be laid out")
			}
			if l.text != "" {
				t.Fatalf("caption must be dropped, got %q", l.text)
			}
			if l.gapX0 != l.gapX1 {
				t.Fatalf("with no caption the rail must be unbroken, got a gap [%v,%v]", l.gapX0, l.gapX1)
			}
			if l.x0 != tc.start || l.x1 != tc.end {
				t.Fatalf("rail must still span the axis, got [%v,%v]", l.x0, l.x1)
			}
		})
	}
}

func TestBrushHint_NoAxisNoHint(t *testing.T) {
	for _, tc := range []struct {
		name            string
		start, end, hgt float32
	}{
		{"zero-width axis", 100, 100, 14},
		{"inverted axis", 200, 100, 14},
		{"zero-height strip", brushAxisStart, brushAxisEnd, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := hintLayout(t, tc.start, tc.end, tc.hgt, defaultBrushHintText); ok {
				t.Fatal("expected no layout")
			}
		})
	}
}

// The rail outlives a committed range and the caption does not — the split
// imztop's availability strip turns on, where the replay window is mirrored
// onto the brush every frame and the strip is never idle.
func TestBrushHint_CaptionYieldsToARangeButTheRailDoesNot(t *testing.T) {
	inst, _, _ := brushFixture(t)
	if got := inst.brushHintText(); got != defaultBrushHintText {
		t.Fatalf("an empty track must carry the caption, got %q", got)
	}

	// Set programmatically, as a caller mirroring an external range does: the
	// user never made this gesture, so the strip still has to say it is one.
	inst.SetBrush(brushViewMinMS, brushViewMinMS+10_000)
	if got := inst.brushHintText(); got != "" {
		t.Fatalf("a range on the track must drop the caption, got %q", got)
	}
	l, ok := computeBrushHintLayout(brushTestLayout(), 14, inst.brushHintText())
	if !ok || l.x0 != brushAxisStart || l.x1 != brushAxisEnd {
		t.Fatalf("the rail must still span the axis under a range, got [%v,%v] ok=%v", l.x0, l.x1, ok)
	}

	inst.ClearBrush()
	if got := inst.brushHintText(); got != defaultBrushHintText {
		t.Fatalf("clearing must bring the caption back, got %q", got)
	}
}

// Mid-gesture the pending fill is the thing to follow; a caption under it is
// noise.
func TestBrushHint_CaptionDropsWhileGestureInFlight(t *testing.T) {
	inst, _, _ := brushFixture(t)
	inst.driveBrush(brushFramePress, 200)
	if got := inst.brushHintText(); got != "" {
		t.Fatalf("a gesture in flight must drop the caption, got %q", got)
	}
}
