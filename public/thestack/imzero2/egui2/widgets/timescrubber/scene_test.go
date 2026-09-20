package timescrubber

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// scene drives a scrubber with no host behind it: a frame's input is
// scripted into the registers the strip reads, and Render runs against a
// channel that discards what it paints.
type scene struct {
	t            *testing.T
	sm           *c.StateManager
	sc           *Scrubber
	steps        []Step
	canvas, area widgethandle.WidgetHandle
	clock        time.Time
}

const sceneW = 1000

func newScene(t *testing.T, steps []Step, opts Options) (s *scene) {
	reset := scenetest.Install()
	t.Cleanup(reset)
	ids := c.NewWidgetIdStack()
	opts.NoKeyboard = true // no key frame, so the handles derive as below
	if opts.ScopeKey == "" {
		opts.ScopeKey = "scene"
	}
	s = &scene{t: t, sm: c.CurrentApplicationState.StateManager, steps: steps,
		clock: time.Date(2026, 3, 1, 0, 30, 0, 0, time.UTC)}
	s.sc = New(ids, opts)
	s.sc.now = func() time.Time { return s.clock }
	s.sc.Transport.Dwell = -1
	for range c.IdScope(ids.PrepareStr(opts.ScopeKey)) {
		s.canvas = widgethandle.Make(ids.PrepareStr(canvasKey).Derive())
		s.area = widgethandle.Make(ids.PrepareStr(areaKey).Derive())
	}
	s.frame(0, 0, 0, 0, 0) // the first frame has no canvas yet
	return
}

// frame plays one frame: the area's flags, the pointer at (x, y), pressed at
// (ox, oy).
func (s *scene) frame(flags c.ResponseFlagsE, x, y, ox, oy float32) Events {
	s.sm.ScriptReset()
	s.sm.ScriptCanvasCursor(s.canvas, c.CanvasCursorValue{PosX: x, PosY: y})
	s.sm.ScriptCanvasCursor(s.area, c.CanvasCursorValue{PosX: ox, PosY: oy})
	s.sm.ScriptPointer(c.PointerValue{X: x, Y: y, Valid: true})
	s.sm.ScriptResponse(s.area, flags)
	s.clock = s.clock.Add(16 * time.Millisecond)
	return s.sc.Render(sceneW, s.steps)
}

func (s *scene) x(step float64) float32 {
	return newAxis(s.steps, padX, sceneW-padX, s.sc.Opts.ByIndex).posToX(step)
}

// drag presses at (from, y), moves to `to` and lets go.
func (s *scene) drag(from, to, y float32) (last Events) {
	s.frame(c.DragStartedResponseFlags|c.DraggedResponseFlags, from+1, y, from, y)
	s.frame(c.DraggedResponseFlags, (from+to)/2, y, from, y)
	s.frame(c.DraggedResponseFlags, to, y, from, y)
	return s.frame(c.DragStoppedResponseFlags, to, y, from, y)
}

func (s *scene) rng() [2]int {
	if !s.sc.Transport.RangeOn {
		return [2]int{-1, -1}
	}
	lo, hi := s.sc.Transport.Bounds(len(s.steps))
	return [2]int{lo, hi}
}

const (
	onBandY = 5
	onBodyY = 60
)

// The band is the range's alone (ADR-0251 §SD5; survey rows 31, 32).
func TestTheBandDoesNotSeekAndASliverIsNoRange(t *testing.T) {
	s := newScene(t, stepsAt(0, 1, 2, 3, 4, 5, 6, 7, 8, 9), Options{})
	tr := &s.sc.Transport
	tr.Seek(2, 10)
	tr.SetRange(3, 6)

	s.frame(c.PrimaryClickedResponseFlags, s.x(8), onBandY, s.x(8), onBandY)
	assert.Equal(t, 2.0, tr.Pos, "a click on the band is not a seek")
	s.frame(c.PrimaryClickedResponseFlags|c.DoubleClickedResponseFlags, s.x(8), onBandY, s.x(8), onBandY)
	assert.Equal(t, 2.0, tr.Pos, "and so a double click clears the range and moves nothing")
	assert.False(t, tr.RangeOn)

	tr.SetRange(3, 6)
	s.drag(s.x(8), s.x(8)+2, onBandY)
	assert.Equal(t, [2]int{3, 6}, s.rng(), "a drag shorter than a step neither makes a range nor clears one")

	ev := s.frame(c.PrimaryClickedResponseFlags, s.x(7), onBodyY, s.x(7), onBodyY)
	assert.Equal(t, 7.0, tr.Pos, "a click on the body seeks")
	assert.True(t, ev.Settled && ev.Moved)
	assert.Equal(t, 7, ev.Step)
}

// A range is made, its edges dragged, its body slid (ADR-0251 §SD5).
func TestTheRangeIsEditedOnTheBand(t *testing.T) {
	s := newScene(t, stepsAt(0, 1, 2, 3, 4, 5, 6, 7, 8, 9), Options{})
	s.drag(s.x(6), s.x(2), onBandY)
	assert.Equal(t, [2]int{2, 6}, s.rng(), "in either direction")

	s.drag(s.x(2), s.x(9), onBandY)
	assert.Equal(t, [2]int{5, 6}, s.rng(), "the lower edge stops one short of the upper and does not pass it")
	s.drag(s.x(6), s.x(8), onBandY)
	assert.Equal(t, [2]int{5, 8}, s.rng(), "the upper edge")

	s.drag(s.x(6.4), s.x(4.4), onBandY)
	assert.Equal(t, [2]int{3, 6}, s.rng(), "the body slides it whole")
	s.drag(s.x(4.4), s.x(-3), onBandY)
	assert.Equal(t, [2]int{0, 3}, s.rng(), "as far as the series lets it")

	s.drag(s.x(7), s.x(9), onBandY)
	assert.Equal(t, [2]int{7, 9}, s.rng(), "a drag off the range makes a new one")
	assert.False(t, s.sc.Transport.Playing)
}

// The playhead follows a drag and settles where it is let go (ADR-0251 §SD3).
func TestAPlayheadDragSettlesOnRelease(t *testing.T) {
	s := newScene(t, stepsAt(0, 1, 2, 3, 6, 9, 12), Options{})
	tr := &s.sc.Transport
	tr.Toggle(7)
	from, to := s.x(1), s.x(4.4)
	ev := s.frame(c.DragStartedResponseFlags|c.DraggedResponseFlags, from+1, onBodyY, from, onBodyY)
	assert.True(t, ev.Dragging && !ev.Settled)
	assert.False(t, tr.Playing, "a person who moves the playhead wants to look at it")
	ev = s.frame(c.DraggedResponseFlags, to, onBodyY, from, onBodyY)
	assert.InDelta(t, 4.4, tr.Pos, 0.02)
	assert.False(t, ev.Settled, "a position under a drag has not come to rest")
	ev = s.frame(c.DragStoppedResponseFlags, to, onBodyY, from, onBodyY)
	assert.Equal(t, 4.0, tr.Pos)
	assert.True(t, ev.Settled)
	assert.Equal(t, 4, ev.Step)

	// A press on the body that drifts up onto the band is still the playhead's.
	s.drag(s.x(2), s.x(5), onBodyY)
	s.frame(c.DragStartedResponseFlags|c.DraggedResponseFlags, s.x(3), onBandY, s.x(2), onBodyY)
	assert.False(t, tr.RangeOn, "the press origin decides the gesture")
}

// A rebuilt series keeps the time and not the index (ADR-0251 §SD2).
func TestTheInstantSurvivesAChangeOfSteps(t *testing.T) {
	s := newScene(t, stepsAt(0, 1, 2, 3, 4, 5, 6), Options{})
	tr := &s.sc.Transport
	tr.Seek(4, 7) // 04:00
	tr.SetRange(2, 6)
	s.frame(0, 0, 0, 0, 0)

	s.steps = stepsAt(0, 2, 4, 6, 8) // the same span, two-hourly
	ev := s.frame(0, 0, 0, 0, 0)
	assert.Equal(t, 2.0, tr.Pos, "04:00 is step 2 now")
	assert.True(t, ev.Settled && ev.Moved)
	assert.Equal(t, [2]int{1, 3}, s.rng(), "and the range is 02:00 to 06:00 still")

	s.steps = stepsAt(10, 11, 12)
	s.frame(0, 0, 0, 0, 0)
	assert.Equal(t, 0.0, tr.Pos, "an instant before the series is its first step")
}

// A scrubber drawn twice in a frame advances once (survey row 23).
func TestDrawnTwiceItAdvancesOnce(t *testing.T) {
	s := newScene(t, stepsAt(0, 1, 2, 3, 4), Options{})
	for i := range s.steps {
		s.steps[i].State = StepStateHeld
	}
	tr := &s.sc.Transport
	tr.Rate = 1
	tr.Toggle(5)
	s.frame(0, 0, 0, 0, 0)
	once := tr.Pos
	s.sm.ScriptReset()
	s.sc.Render(sceneW, s.steps) // the same clock: no time has passed
	assert.Equal(t, once, tr.Pos)
	assert.InDelta(t, 0.016, once, 1e-6)
}

// Nothing, one step, the compact form, the index axis: every form draws.
func TestEveryFormDraws(t *testing.T) {
	for _, opts := range []Options{{}, {Compact: true}, {ByIndex: true}, {NoSnap: true, ValueName: "speed", ValueUnit: "m/s"}} {
		for _, steps := range [][]Step{nil, stepsAt(3), stepsAt(0, 1), stepsAt(0, 0, 0), stepsAt(0, 1, 2, 3, 6, 9)} {
			s := newScene(t, steps, opts)
			s.sc.Marks = []Mark{{At: time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC), Label: "run"}}
			for i := range steps {
				steps[i].Value, steps[i].Peak = float32(i+1), float32(3*(i+1))
				steps[i].State = StepStateE(i % 4)
			}
			s.sc.Transport.SetRange(1, 3)
			s.frame(c.HoveredResponseFlags, 300, onBodyY, 300, onBodyY)
			s.frame(c.HoveredResponseFlags, 300, onBandY, 300, onBandY)
			s.drag(100, 700, onBodyY)
		}
	}
}

// What a crowded strip costs follows its width, not its step count (survey
// row 40).
func TestACrowdedStripCostsItsWidth(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	count := func(n int) int {
		hours := make([]float64, n)
		for i := range hours {
			hours[i] = float64(i)
		}
		steps := stepsAt(hours...)
		for i := range steps {
			steps[i].Value, steps[i].State = float32(i%17), StepStateE(i%4)
		}
		sc := New(c.NewWidgetIdStack(), Options{NoKeyboard: true, ScopeKey: "crowd"})
		sm := c.CurrentApplicationState.StateManager
		sm.ScriptReset()
		sc.Render(sceneW, steps)
		sm.ScriptReset()
		zero()
		sc.Render(sceneW, steps)
		return messages()
	}
	few, many := count(2000), count(20000)
	assert.LessOrEqual(t, many, few+few/4, "2 000 steps cost %d messages, 20 000 cost %d", few, many)
	assert.Less(t, few, 4*sceneW)
}

func TestColumnsKeepTheExtremes(t *testing.T) {
	hours := make([]float64, 3000)
	for i := range hours {
		hours[i] = float64(i)
	}
	steps := stepsAt(hours...)
	nan := float32(math.NaN())
	for i := range steps {
		steps[i].Value, steps[i].Peak, steps[i].State = 1, nan, StepStateHeld
	}
	steps[1234].Value, steps[1234].Peak = 9, 40
	steps[2222].State = StepStateMissing
	steps[2223].State = StepStateLoading
	axis := newAxis(steps, padX, sceneW-padX, false)
	require.True(t, crowded(axis))
	cols := columnsOf(axis, steps, nil)
	assert.LessOrEqual(t, len(cols), (sceneW-2*padX)/columnW+1)
	var top, peak float32
	missing, covered := 0, 0
	for _, col := range cols {
		top, peak = maxNum(top, col.value), maxNum(peak, col.peak)
		if col.state == StepStateMissing {
			missing++
		}
		covered += col.last - col.first + 1
	}
	assert.Equal(t, float32(9), top, "the step a reader scans for is not the one left out")
	assert.Equal(t, float32(40), peak)
	assert.Equal(t, 1, missing, "the worst state in a column is the column's")
	assert.Equal(t, len(steps), covered, "every step is in a column")

	// Whether a strip is crowded is the axis's to say and nothing else's.
	assert.False(t, crowded(newAxis(stepsAt(0, 1, 2, 3, 6, 9), padX, sceneW-padX, false)))
}

func TestBarsAreScaledToTheValues(t *testing.T) {
	steps := stepsAt(0, 1, 2)
	steps[0].Value, steps[0].Peak = 4, 30
	steps[1].Value, steps[1].Peak = 8, 12
	assert.InDelta(t, 8*1.04, valueScale(steps), 1e-5, "gusts well above the means do not put the bars in the bottom of the strip")
	nan := float32(math.NaN())
	steps[2].Value, steps[2].Peak = nan, nan
	assert.InDelta(t, 8*1.04, valueScale(steps), 1e-5, "a step without a value does not cost the others their bars")
	for i := range steps {
		steps[i].Value = nan
	}
	assert.InDelta(t, 30*1.04, valueScale(steps), 1e-4, "a series of peaks alone is scaled to them")
}

// A fixed wall clock is a fixed now line, not a stopped transport: the two
// clocks are different questions (ADR-0251 §SD8).
func TestAFixedWallClockStillPlays(t *testing.T) {
	at := time.Date(2026, 3, 1, 2, 20, 0, 0, time.UTC)
	s := newScene(t, stepsAt(0, 1, 2, 3, 4, 5), Options{Now: func() time.Time { return at }})
	for i := range s.steps {
		s.steps[i].State = StepStateHeld
	}
	tr := &s.sc.Transport
	tr.Rate = 4
	tr.Toggle(len(s.steps))
	for range 10 {
		s.frame(0, 0, 0, 0, 0)
	}
	assert.Greater(t, tr.Pos, 0.3, "playback runs on elapsed real time, whatever the wall clock says")
	step, spans := s.sc.nowStep(s.steps)
	assert.True(t, spans)
	assert.Equal(t, 2, step, "and the now line stands at the step nearest where the fixed clock puts it")
}
