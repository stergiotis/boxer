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
	tr := Transport{Rate: 1, Playing: true, Dwell: -1}
	for range 10 { // 2.5 s at one step a second, over four steps
		tr.Advance(0.25, 4, always)
	}
	assert.InDelta(t, 2.5, tr.Pos, 1e-9)
	for range 4 {
		tr.Advance(0.25, 4, always)
	}
	assert.InDelta(t, 0.5, tr.Pos, 1e-9, "a loop starts over at the first step")

	tr = Transport{Rate: 1, Playing: true, Mode: ModeBounce, Pos: 2.5, Dwell: -1}
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
	tr := Transport{Rate: 2, Playing: true, Dwell: -1}
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

// Playback holds the last step before a loop wraps, and each end before a
// bounce turns (ADR-0251 §SD3): without it the last step is on screen for one
// frame.
func TestPlaybackDwellsAtTheEnds(t *testing.T) {
	tr := Transport{Rate: 1, Playing: true, Pos: 2.5, Dwell: 0.5}
	tr.Advance(0.25, 4, always)
	tr.Advance(0.25, 4, always)
	assert.Equal(t, 3.0, tr.Pos)
	assert.True(t, tr.TakeSettled(), "arriving on a step settles")
	tr.Advance(0.25, 4, always)
	assert.Equal(t, 3.0, tr.Pos, "held")
	assert.True(t, tr.Dwelling())
	tr.Advance(0.25, 4, always)
	tr.Advance(0.25, 4, always)
	assert.InDelta(t, 0.25, tr.Pos, 1e-9, "then round again, and the time past the dwell is played")
	assert.False(t, tr.Dwelling())

	// A once-through stops and does not dwell; a seek lets go of a dwell.
	tr = Transport{Rate: 1, Playing: true, Pos: 2.9, Dwell: 5, Mode: ModeOnce}
	tr.Advance(0.25, 4, always)
	assert.False(t, tr.Playing)
	tr = Transport{Rate: 1, Playing: true, Pos: 3, Dwell: 5}
	tr.Advance(0.1, 4, always)
	require.True(t, tr.Dwelling())
	tr.Seek(1, 4)
	assert.False(t, tr.Dwelling())

	// The literal order of a bounce over four steps, by the steps it settles on.
	tr = Transport{Rate: 4, Playing: true, Mode: ModeBounce, Dwell: 0.2}
	var visited []int
	for range 80 {
		tr.Advance(0.05, 4, always)
		if tr.TakeSettled() {
			visited = append(visited, int(math.Round(tr.Pos)))
		}
	}
	require.GreaterOrEqual(t, len(visited), 9)
	assert.Equal(t, []int{1, 2, 3, 2, 1, 0, 1, 2, 3}, visited[:9], "each end once per turn")
}

// Play pressed in the middle of a bracket whose far end is not there waits
// where it is (ADR-0251 §SD3).
func TestPlaybackFromMidBracketWaitsToo(t *testing.T) {
	held := map[int]bool{2: true}
	ready := func(step int) bool { return held[step] }
	tr := Transport{Rate: 1, Pos: 2.4, Dwell: -1}
	tr.Toggle(6)
	tr.Advance(0.25, 6, ready)
	assert.True(t, tr.Buffering)
	assert.Equal(t, 2.4, tr.Pos)
	assert.Equal(t, 3, tr.BufferingStep, "it names the step it waits for")
	tr.Advance(0.25, 6, ready)
	assert.InDelta(t, 0.5, tr.BufferingFor, 1e-9)
	assert.Greater(t, tr.WaitShare, 0.0)
	held[3] = true
	tr.Advance(0.25, 6, ready)
	assert.False(t, tr.Buffering)
	assert.InDelta(t, 2.65, tr.Pos, 1e-9)
	assert.Zero(t, tr.BufferingFor)
}

// Ahead lists what Advance will reach, in the order it reaches it.
func TestAheadWalksTheRulesAdvanceDoes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		steps := rapid.IntRange(2, 12).Draw(t, "steps")
		// A dwell longer than a frame, so that an end is seen before the wrap.
		tr := Transport{Rate: 4, Playing: true, Dwell: 0.06,
			Mode: AllModes[rapid.IntRange(0, len(AllModes)-1).Draw(t, "mode")],
			Pos:  float64(rapid.IntRange(0, steps-1).Draw(t, "pos"))}
		if rapid.Bool().Draw(t, "range") {
			tr.SetRange(rapid.IntRange(0, steps-1).Draw(t, "a"), rapid.IntRange(0, steps-1).Draw(t, "b"))
		}
		lo, hi := tr.Bounds(steps)
		tr.Pos = clampPos(tr.Pos, lo, hi)
		n := rapid.IntRange(1, 6).Draw(t, "n")
		ahead := tr.Ahead(steps, n, nil)

		at := int(tr.Pos)
		inHand := at // the step it starts on is held already and is not listed
		var reached []int
		tr.TakeSettled()
		for i := 0; i < 400 && tr.Playing && len(reached) < len(ahead); i++ {
			tr.Advance(0.05, steps, always)
			if tr.TakeSettled() && int(math.Round(tr.Pos)) != at {
				at = int(math.Round(tr.Pos))
				if at != inHand {
					reached = append(reached, at)
				}
			}
		}
		if tr.Mode != ModeOnce && hi-lo >= 1 && len(ahead) == 0 && hi-lo > 1 {
			t.Fatalf("nothing ahead of %v in %v over [%d,%d]", tr.Pos, tr.Mode, lo, hi)
		}
		for i := range reached {
			if reached[i] != ahead[i] {
				t.Fatalf("ahead %v, reached %v", ahead, reached)
			}
		}
		for i, s := range ahead {
			if s < lo || s > hi {
				t.Fatalf("step %d of %v is outside [%d,%d]", i, ahead, lo, hi)
			}
		}
	})

	// At rest it is the way forward, so play finds the next step there.
	tr := Transport{Pos: 2}
	assert.Equal(t, []int{3, 4}, tr.Ahead(6, 2, nil))
	tr = Transport{Pos: 4.5, Mode: ModeOnce}
	assert.Empty(t, tr.Ahead(6, 3, nil), "a once-through has nothing past its end")
	tr = Transport{Pos: 4.5}
	assert.Equal(t, []int{0, 1, 2}, tr.Ahead(6, 3, nil), "a loop wraps")
	tr = Transport{Pos: 4.5, Mode: ModeBounce}
	assert.Equal(t, []int{3, 2}, tr.Ahead(6, 2, nil), "a bounce turns")
}

// The ends of a range are set from the playhead, the range slides whole, and
// an edge stops short of the other (ADR-0251 §SD5).
func TestTheRangeIsEdited(t *testing.T) {
	var tr Transport
	tr.SetIn(3, 10)
	lo, hi := tr.Bounds(10)
	assert.Equal(t, [2]int{3, 9}, [2]int{lo, hi}, "in alone runs to the end")
	tr.SetOut(6, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{3, 6}, [2]int{lo, hi})
	tr.SetIn(8, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{8, 9}, [2]int{lo, hi}, "an in past the out sends the out to the end")
	tr.SetOut(2, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{0, 2}, [2]int{lo, hi})

	tr.SetRange(3, 6)
	tr.SlideRange(2, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{5, 8}, [2]int{lo, hi})
	tr.SlideRange(9, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{6, 9}, [2]int{lo, hi}, "as far as the series lets it, length kept")
	tr.SlideRange(-20, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{0, 3}, [2]int{lo, hi})

	tr.MoveRangeEdge(false, 7, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{2, 3}, [2]int{lo, hi}, "an edge stops one short of the other")
	tr.MoveRangeEdge(true, 0, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{2, 3}, [2]int{lo, hi})
	tr.MoveRangeEdge(true, 40, 10)
	lo, hi = tr.Bounds(10)
	assert.Equal(t, [2]int{2, 9}, [2]int{lo, hi})

	// A range set around a resting playhead leaves it alone (survey row 26).
	tr = Transport{Pos: 5}
	tr.SetRange(3, 8)
	assert.Equal(t, 5.0, tr.Pos)
}

// No steps, one step, two: nothing spins and nothing moves that should not.
func TestDegenerateSeries(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		for _, m := range AllModes {
			tr := Transport{Playing: true, Mode: m, Rate: 4, Dwell: -1}
			for range 50 {
				tr.Advance(0.1, n, always)
			}
			tr.StepBy(1, n)
			tr.First(n)
			tr.Last(n)
			tr.SetIn(0, n)
			tr.SetOut(0, n)
			tr.SlideRange(1, n)
			assert.LessOrEqual(t, tr.Pos, float64(max(n-1, 0)))
			assert.LessOrEqual(t, len(tr.Ahead(n, 3, nil)), 1)
		}
	}
	// Times that stand still, run backward, or leap do not move it far.
	tr := Transport{Playing: true, Rate: 1, Dwell: -1}
	for _, dt := range []float64{0, -1, 1e-3, 10} {
		tr.Advance(dt, 100, always)
	}
	assert.LessOrEqual(t, tr.Pos, 0.3, "a stalled frame loop resumes where it was")
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
