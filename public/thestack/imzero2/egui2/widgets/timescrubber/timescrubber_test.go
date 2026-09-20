package timescrubber

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func always(int) bool { return true }

func TestPlaybackLoopsBouncesAndStops(t *testing.T) {
	tr := Transport{Rate: 1, Playing: true}
	for range 10 { // 2.5 s at one step a second, over four steps
		tr.Advance(0.25, 4, always)
	}
	assert.InDelta(t, 2.5, tr.Pos, 1e-9)
	for range 4 {
		tr.Advance(0.25, 4, always)
	}
	assert.InDelta(t, 0.5, tr.Pos, 1e-9, "a loop starts over at the first step")

	tr = Transport{Rate: 1, Playing: true, Mode: ModeBounce, Pos: 2.5}
	for range 4 {
		tr.Advance(0.25, 4, always)
	}
	assert.InDelta(t, 2.5, tr.Pos, 1e-9, "half a step to the end and half a step back")
	for range 12 {
		tr.Advance(0.25, 4, always)
	}
	assert.InDelta(t, 0.5, tr.Pos, 1e-9, "and round again at the start")
	assert.True(t, tr.Playing)

	tr = Transport{Rate: 1, Playing: true, Mode: ModeOnce, Pos: 2.5}
	for range 8 {
		tr.Advance(0.25, 4, always)
	}
	assert.Equal(t, 3.0, tr.Pos)
	assert.False(t, tr.Playing, "once stops at the end")
	tr.Toggle(4)
	assert.Equal(t, 0.0, tr.Pos, "and starts over when played again from there")
}

// Playback enters a bracket only when both its steps are there, and waits
// just inside it, so that whoever loads the data sees the bracket wanted
// (ADR-0251 §SD3).
func TestPlaybackWaitsForTheNextStep(t *testing.T) {
	held := map[int]bool{0: true, 1: true}
	ready := func(step int) bool { return held[step] }
	tr := Transport{Rate: 1, Playing: true}
	for range 8 {
		tr.Advance(0.25, 5, ready)
	}
	assert.True(t, tr.Buffering)
	assert.InDelta(t, 1, tr.Pos, entrance, "it waits at step 1")
	assert.Greater(t, tr.Pos, 1.0, "inside the bracket, so step 2 is asked for")
	assert.Less(t, tr.Pos-1, 1e-3, "and close enough that the picture is step 1's")

	held[2] = true
	tr.Advance(0.25, 5, ready)
	assert.False(t, tr.Buffering)
	assert.InDelta(t, 1.25, tr.Pos, 1e-3)

	// Mid-bracket it does not stop for a step that went away: the bracket
	// was entered whole, and the far end is the loader's to bring back.
	delete(held, 2)
	tr.Advance(0.25, 5, ready)
	assert.False(t, tr.Buffering)
	assert.InDelta(t, 1.5, tr.Pos, 1e-3)
}

func TestARangeBoundsPlaybackAndTheEnds(t *testing.T) {
	tr := Transport{Rate: 2, Playing: true}
	tr.SetRange(6, 3)
	lo, hi := tr.Bounds(10)
	assert.Equal(t, [2]int{3, 6}, [2]int{lo, hi})
	for range 40 {
		tr.Advance(0.1, 10, always)
		require.GreaterOrEqual(t, tr.Pos, 3.0)
		require.LessOrEqual(t, tr.Pos, 6.0)
	}
	tr.First(10)
	assert.Equal(t, 3.0, tr.Pos)
	assert.False(t, tr.Playing)
	tr.Last(10)
	assert.Equal(t, 6.0, tr.Pos)

	tr.SetRange(4, 4)
	assert.False(t, tr.RangeOn, "one step is no range")
	tr.SetRange(2, 50)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{2, 9}, [2]int{lo, hi}, "a range is clamped to the steps there are")
}

func TestSteppingFromBetweenTwoSteps(t *testing.T) {
	tr := Transport{Pos: 2.4, Playing: true}
	tr.StepBy(1, 6)
	assert.Equal(t, 3.0, tr.Pos)
	assert.False(t, tr.Playing, "a person who moves the playhead wants to look at it")
	tr.Pos = 2.4
	tr.StepBy(-1, 6)
	assert.Equal(t, 2.0, tr.Pos)
	tr.StepBy(-1, 6)
	assert.Equal(t, 1.0, tr.Pos)
	tr.StepBy(-9, 6)
	assert.Equal(t, 0.0, tr.Pos)
	tr.StepBy(9, 6)
	assert.Equal(t, 5.0, tr.Pos)
}

func stepsAt(hours ...float64) (steps []Step) {
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, h := range hours {
		steps = append(steps, Step{At: t0.Add(time.Duration(h * float64(time.Hour)))})
	}
	return
}

// Steps sit at their own instants: hourly steps are closer together than the
// three-hourly ones after them (ADR-0251 §SD1).
func TestStepsSitAtTheirOwnTimes(t *testing.T) {
	axis := newTimeAxis(stepsAt(0, 1, 2, 3, 6, 9, 12), 0, 1200)
	assert.InDelta(t, 100, axis.posToX(1), 1e-3)
	assert.InDelta(t, 300, axis.posToX(3), 1e-3)
	assert.InDelta(t, 600, axis.posToX(4), 1e-3)
	assert.InDelta(t, 450, axis.posToX(3.5), 1e-3, "between two steps a position is linear in time")
	assert.InDelta(t, 3.5, axis.xToPos(450), 1e-9)
	assert.Equal(t, 0.0, axis.xToPos(-50))
	assert.Equal(t, 6.0, axis.xToPos(5000))
	assert.Equal(t, "04:30", axis.timeAt(3.5, time.UTC).Format("15:04"))

	// Times that do not increase have no time axis; the steps are spread by index.
	flat := newTimeAxis(stepsAt(0, 0, 0), 0, 100)
	assert.True(t, flat.byIndex)
	assert.InDelta(t, 50, flat.posToX(1), 1e-3)
	assert.InDelta(t, 1.0, flat.xToPos(50), 1e-9)

	one := newTimeAxis(stepsAt(5), 0, 100)
	assert.InDelta(t, 50, one.posToX(0), 1e-3)
	assert.Equal(t, 0.0, one.xToPos(80))
}

func TestPixelAndPositionRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 60).Draw(t, "steps")
		hours := make([]float64, n)
		at := 0.0
		for i := range hours {
			at += rapid.Float64Range(0.25, 12).Draw(t, "gap")
			hours[i] = at
		}
		axis := newTimeAxis(stepsAt(hours...), 16, 16+rapid.Float32Range(50, 2000).Draw(t, "width"))
		pos := rapid.Float64Range(0, float64(n-1)).Draw(t, "pos")
		back := axis.xToPos(axis.posToX(pos))
		if math.Abs(back-pos) > 1e-3 {
			t.Fatalf("position %v came back as %v", pos, back)
		}
		for i := 1; i < n; i++ {
			if !(axis.posToX(float64(i)) > axis.posToX(float64(i-1))) {
				t.Fatalf("step %d is not to the right of step %d", i, i-1)
			}
		}
	})
}

func TestBarsAreAsWideAsTheNarrowestGapAllows(t *testing.T) {
	assert.InDelta(t, maxBarW, barWidth(newTimeAxis(stepsAt(0, 3, 6), 0, 900)), 1e-3)
	assert.InDelta(t, 20*0.72, barWidth(newTimeAxis(stepsAt(0, 1, 2, 3, 6, 9, 12), 0, 240)), 1e-3, "the hourly gap decides, not the three-hourly one")
	assert.InDelta(t, minBarW, barWidth(newTimeAxis(stepsAt(0, 0.01, 12), 0, 300)), 1e-3)
}
