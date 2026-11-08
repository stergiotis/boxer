package trailviewer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timescrubber"
)

// The context extent is five times the window, centred, and never ends
// past now plus one step: a relative window therefore sits in the right
// fifth.
func TestTheContextExtentIsCentredAndClampedAtNow(t *testing.T) {
	T := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	step := 5 * time.Minute
	cf, ct, clamped := contextExtent(T, T.Add(time.Hour), T.Add(48*time.Hour), step)
	assert.False(t, clamped)
	assert.Equal(t, T.Add(-2*time.Hour), cf, "centred: two spans before")
	assert.Equal(t, T.Add(3*time.Hour), ct, "two spans after")

	cf, ct, clamped = contextExtent(T, T.Add(time.Hour), T.Add(time.Hour), step)
	assert.True(t, clamped)
	assert.Equal(t, T.Add(time.Hour).Add(step), ct, "the end is now plus one step")
	assert.Equal(t, 5*time.Hour, ct.Sub(cf), "the span is kept")

	now := T.Add(10 * time.Hour)
	from, to := (Window{Last: time.Hour}).Resolve(now)
	cf, ct, clamped = contextExtent(from, to, now, step)
	assert.True(t, clamped)
	fifth := ct.Sub(cf) / 5
	assert.False(t, from.Before(ct.Add(-fifth).Add(-step)), "a relative window lies in the right fifth")
	assert.Equal(t, now.Add(step), ct)
}

// A window moving inside the middle three fifths keeps the extent; one
// leaving it, or one whose span ratio leaves [3, 8], recomputes. A clamped
// extent's right test is its own end.
func TestTheContextExtentHasHysteresis(t *testing.T) {
	T := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	st := contextState{from: T.Add(-2 * time.Hour), to: T.Add(3 * time.Hour), has: true}
	assert.False(t, st.stale(T, T.Add(time.Hour)), "the window it was placed for")
	assert.False(t, st.stale(T.Add(-30*time.Minute), T.Add(30*time.Minute)), "moved, still inside the middle three fifths")
	assert.False(t, st.stale(T.Add(-time.Hour), T), "at the left edge of the middle three fifths")
	assert.False(t, st.stale(T.Add(time.Hour), T.Add(2*time.Hour)), "at the right edge")
	assert.True(t, st.stale(T.Add(-90*time.Minute), T.Add(-30*time.Minute)), "left past the first fifth")
	assert.True(t, st.stale(T.Add(90*time.Minute), T.Add(150*time.Minute)), "right into the last fifth")
	assert.True(t, st.stale(T, T.Add(2*time.Hour)), "ratio 2.5 is under 3")
	assert.True(t, st.stale(T, T.Add(30*time.Minute)), "ratio 10 is over 8")
	assert.False(t, st.stale(T, T.Add(100*time.Minute)), "ratio 3 holds")
	assert.True(t, (contextState{}).stale(T, T.Add(time.Hour)), "no extent yet")

	clamped := contextState{from: T.Add(-4 * time.Hour), to: T.Add(time.Hour), clamped: true, has: true}
	assert.False(t, clamped.stale(T, T.Add(time.Hour)), "a relative window sits in the right fifth of a clamped extent and keeps it")
	assert.True(t, clamped.stale(T.Add(time.Minute), T.Add(61*time.Minute)), "until now passes the extent's end")

	inst := newInstance(manifest)
	inst.window = Window{Last: time.Hour}
	now := T
	inst.updateContext(now)
	first := inst.ctx
	require.True(t, first.has)
	inst.updateContext(now.Add(time.Minute))
	assert.Equal(t, first, inst.ctx, "a minute later the live window is still inside")
	inst.window = Window{From: T.Add(-10 * time.Hour), To: T.Add(-9 * time.Hour)}
	inst.updateContext(now)
	assert.NotEqual(t, first, inst.ctx, "a window outside recomputes")
	assert.False(t, inst.ctx.clamped)
	assert.Equal(t, T.Add(-12*time.Hour), inst.ctx.from)
}

func bandFixture(T time.Time, step time.Duration, n int) (steps []timescrubber.Step) {
	for i := range n {
		steps = append(steps, timescrubber.Step{At: T.Add(time.Duration(i) * step), State: timescrubber.StepStateHeld})
	}
	return
}

// A window not on bucket boundaries snaps to the enclosing buckets, and the
// range maps back to exactly that window.
func TestStepsAndTheWindowMapBothWays(t *testing.T) {
	T := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	step := 5 * time.Minute
	steps := bandFixture(T, step, 12)
	assert.Equal(t, -1, stepIndexOf(nil, T))
	assert.Equal(t, 0, stepIndexOf(steps, T.Add(-time.Hour)), "before the first step: the first")
	assert.Equal(t, 11, stepIndexOf(steps, T.Add(time.Hour)), "after the last: the last")
	assert.Equal(t, 1, stepIndexOf(steps, T.Add(7*time.Minute)))
	assert.Equal(t, 1, stepIndexOf(steps, T.Add(5*time.Minute)), "on a boundary: the bucket starting there")
	lo, hi := windowToRange(steps, T.Add(7*time.Minute), T.Add(23*time.Minute))
	assert.Equal(t, 1, lo)
	assert.Equal(t, 4, hi, "the bucket holding the last instant before to")
	w := rangeToWindow(steps, lo, hi, step)
	assert.Equal(t, Window{From: T.Add(5 * time.Minute), To: T.Add(25 * time.Minute)}, w)
	lo2, hi2 := windowToRange(steps, w.From, w.To)
	assert.Equal(t, [2]int{lo, hi}, [2]int{lo2, hi2}, "the snapped window maps back to the same range")
	lo, hi = windowToRange(steps, T.Add(5*time.Minute), T.Add(10*time.Minute))
	assert.Equal(t, [2]int{1, 1}, [2]int{lo, hi}, "one bucket is one step")

	counts := []Bucket{{Bucket: T.Add(10 * time.Minute).Unix(), N: 3}, {Bucket: T.Add(20 * time.Minute).Unix(), N: 1}}
	got := contextSteps(T.Add(2*time.Minute), T.Add(31*time.Minute), step, counts, true, T.Add(12*time.Minute), T.Add(22*time.Minute), nil)
	require.Len(t, got, 7, "aligned down to the step, one step per bucket to the extent's end, empty buckets included")
	assert.Equal(t, T, got[0].At)
	assert.Equal(t, float32(0), got[0].Value)
	assert.Equal(t, float32(3), got[2].Value)
	assert.Equal(t, timescrubber.StepStateHeld, got[1].State)
	assert.Equal(t, timescrubber.StepStateLoading, got[2].State, "a bucket overlapping the wanted window loads while the read is in flight")
	assert.Equal(t, timescrubber.StepStateLoading, got[4].State)
	assert.Equal(t, timescrubber.StepStateHeld, got[5].State)
	assert.Empty(t, contextSteps(T, T.Add(time.Hour), 0, nil, false, T, T, got), "no step, no steps")
}

// The band's commit rule: a changed range while not dragging commits once,
// while dragging never; a cleared range commits the context extent; a
// mirror never commits.
func TestTheBandCommitsOnceAndNeverWhileDragging(t *testing.T) {
	mirrored := bandRange{on: true, lo: 2, hi: 6}
	moved := bandRange{on: true, lo: 3, hi: 7}
	assert.Equal(t, bandNothing, decideBand(true, moved, mirrored, false), "dragging")
	assert.Equal(t, bandNothing, decideBand(true, bandRange{}, mirrored, false), "dragging, even cleared")
	assert.Equal(t, bandCommitRange, decideBand(false, moved, mirrored, false))
	assert.Equal(t, bandNothing, decideBand(false, moved, moved, false), "after the commit the range is what was mirrored: once")
	assert.Equal(t, bandCommitContext, decideBand(false, bandRange{}, mirrored, false), "a cleared range zooms out to the context")
	assert.Equal(t, bandNothing, decideBand(false, bandRange{}, bandRange{}, false), "off stays off")
	assert.Equal(t, bandMirror, decideBand(false, moved, mirrored, true), "the window or the steps changed: the host's turn, no commit")
	assert.Equal(t, bandMirror, decideBand(false, mirrored, mirrored, true))

	var tr timescrubber.Transport
	assert.Equal(t, bandRange{}, rangeOf(&tr, 10))
	tr.SetRange(7, 3)
	assert.Equal(t, bandRange{on: true, lo: 3, hi: 7}, rangeOf(&tr, 10), "normalised")
	tr.SetRange(4, 4)
	assert.Equal(t, bandRange{}, rangeOf(&tr, 10), "one step is no range")
}

// Earlier and Later shift by half the span, Later never past now; Zoom
// doubles or halves about the centre or the selected instant, To at most
// now, in no narrower than ten seconds; Live rounds to the ladder; Fit trims
// to the non-empty context buckets.
func TestTheNavigationRowsArithmetic(t *testing.T) {
	T := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	now := T.Add(90 * time.Minute)
	from, to := T, T.Add(time.Hour)

	w, ok := shiftWindow(from, to, now, -1)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(-30 * time.Minute), To: T.Add(30 * time.Minute)}, w)
	w, ok = shiftWindow(from, to, now, 1)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(30 * time.Minute), To: T.Add(90 * time.Minute)}, w)
	w, ok = shiftWindow(w.From, w.To, now, 1)
	assert.False(t, ok, "at now, Later does nothing")
	w, ok = shiftWindow(T.Add(20*time.Minute), T.Add(80*time.Minute), now, 1)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(30 * time.Minute), To: now}, w, "clamped so To is now, the span kept")
	_, ok = shiftWindow(T, T, now, -1)
	assert.False(t, ok)

	w, ok = zoomWindow(from, to, now, time.Time{}, false, true)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(-30 * time.Minute), To: T.Add(90 * time.Minute)}, w, "doubled about the centre")
	w, ok = zoomWindow(from, to, now.Add(-time.Hour), time.Time{}, false, true)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(-90 * time.Minute), To: T.Add(30 * time.Minute)}, w, "shifted left to now")
	w, ok = zoomWindow(from, to, now, time.Time{}, false, false)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(15 * time.Minute), To: T.Add(45 * time.Minute)}, w, "halved about the centre")
	w, ok = zoomWindow(from, to, now, T.Add(10*time.Minute), true, false)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(5 * time.Minute), To: T.Add(35 * time.Minute)}, w, "halved about the selected row, which keeps its place")
	w, ok = zoomWindow(from, to, now, T.Add(-time.Hour), true, false)
	require.True(t, ok)
	assert.Equal(t, Window{From: T, To: T.Add(30 * time.Minute)}, w, "a selected row outside anchors at the nearer edge")
	w, ok = zoomWindow(T, T.Add(15*time.Second), now, time.Time{}, false, false)
	require.True(t, ok)
	assert.Equal(t, zoomInMin, w.To.Sub(w.From), "no narrower than ten of the smallest step")
	_, ok = zoomWindow(T, T.Add(zoomInMin), now, time.Time{}, false, false)
	assert.False(t, ok, "already there")

	assert.Equal(t, time.Hour, roundLive(time.Hour))
	assert.Equal(t, time.Hour, roundLive(70*time.Minute))
	assert.Equal(t, 30*time.Minute, roundLive(40*time.Minute), "the nearest of the ladder")
	assert.Equal(t, 3*time.Hour, roundLive(2*time.Hour+10*time.Minute), "not larger than one and a half times the span: 3h is")
	assert.Equal(t, 20*time.Second, roundLive(20*time.Second), "under the ladder, the exact span")
	assert.Equal(t, 30*24*time.Hour, roundLive(365*24*time.Hour), "over it, the largest")

	step := 5 * time.Minute
	counts := []Bucket{{Bucket: T.Unix(), N: 0}, {Bucket: T.Add(10 * time.Minute).Unix(), N: 2}, {Bucket: T.Add(25 * time.Minute).Unix(), N: 1}, {Bucket: T.Add(40 * time.Minute).Unix()}}
	w, ok = fitWindow(counts, step)
	require.True(t, ok)
	assert.Equal(t, Window{From: T.Add(10 * time.Minute), To: T.Add(30 * time.Minute)}, w, "the first and last non-empty bucket")
	_, ok = fitWindow([]Bucket{{Bucket: T.Unix()}}, step)
	assert.False(t, ok, "every bucket empty")
	_, ok = fitWindow(nil, step)
	assert.False(t, ok)
}

// Back pops the window applyWindow pushed, and the Back itself pushes
// nothing; the history is bounded.
func TestBackPopsAndDoesNotPush(t *testing.T) {
	inst := newInstance(manifest)
	T := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	a := Window{Last: time.Hour}
	b := Window{From: T, To: T.Add(time.Hour)}
	cw := Window{From: T.Add(-time.Hour), To: T.Add(2 * time.Hour)}
	inst.window = a
	inst.applyWindow(a)
	_, has := inst.history.top()
	assert.False(t, has, "the same window pushes nothing")
	inst.applyWindow(b)
	inst.applyWindow(cw)
	top, has := inst.history.top()
	require.True(t, has)
	assert.True(t, sameWindow(b, top))
	inst.backGesture()
	assert.True(t, sameWindow(b, inst.window), "Back applies the previous window")
	top, has = inst.history.top()
	require.True(t, has)
	assert.True(t, sameWindow(a, top), "and pushes nothing: the next Back is the one before")
	assert.False(t, inst.hasBackTarget)
	inst.backGesture()
	assert.True(t, sameWindow(a, inst.window))
	_, has = inst.history.top()
	assert.False(t, has)
	inst.backGesture()
	assert.True(t, sameWindow(a, inst.window), "an empty history does nothing")

	for i := range historyDepth + 5 {
		inst.applyWindow(Window{From: T.Add(time.Duration(i) * time.Minute), To: T.Add(time.Duration(i+1) * time.Minute)})
	}
	assert.Len(t, inst.history.items, historyDepth)
	assert.True(t, inst.history.items[0].From.After(T), "the oldest were dropped")
}

// The detail strip's intervals: one per (bucket, series), the series as
// lane, the count against the largest as intensity from a floor up.
func TestTheDetailIntervalsComeFromTheBuckets(t *testing.T) {
	T := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	step := time.Minute
	b := []Bucket{{Bucket: T.Unix(), Key: "llmCall", N: 4}, {Bucket: T.Unix(), Key: "auditEvent", N: 1}, {Bucket: T.Add(step).Unix(), Key: "auditEvent", N: 10}}
	ivs, keys := detailIntervals(b, step)
	assert.Equal(t, []string{"auditEvent", "llmCall"}, keys)
	require.Len(t, ivs, 3)
	byLane := map[string][]*layout.IntervalEvent{}
	for _, iv := range ivs {
		byLane[iv.LaneHint] = append(byLane[iv.LaneHint], iv)
		assert.Equal(t, int64(step/time.Millisecond), iv.ToMS-iv.FromMS, "one step wide")
		assert.Equal(t, int32(0), iv.KindID-int32(indexOfKey(keys, iv.LaneHint)), "the kind is the series")
	}
	require.Len(t, byLane["auditEvent"], 2)
	require.Len(t, byLane["llmCall"], 1)
	assert.InDelta(t, 1.0, byLane["auditEvent"][1].Intensity, 1e-6, "the largest count")
	assert.InDelta(t, 0.58, byLane["llmCall"][0].Intensity, 1e-6, "four of ten, on the scale from the floor to one")
	assert.InDelta(t, 0.37, byLane["auditEvent"][0].Intensity, 1e-6, "a bucket of one row is above the floor, so it is visible and still the least")
	assert.Equal(t, "1 row", byLane["auditEvent"][0].Note, "the count is the tooltip's")
	assert.Equal(t, "4 rows", byLane["llmCall"][0].Note)
	ivs, _ = detailIntervals(nil, step)
	assert.Empty(t, ivs)
	assert.Len(t, detailColors(), stripKeys+1)

	bands := outOfHoursBands(func() *time.Location { return time.UTC })
	mon := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC) // a Monday
	var got []layout.BackgroundBand
	for band := range bands(mon.Add(-12*time.Hour).UnixMilli(), mon.Add(36*time.Hour).UnixMilli()) {
		got = append(got, band)
	}
	require.Len(t, got, 4, "Sunday's second half whole, Monday's two spans, Tuesday's first")
	assert.Equal(t, mon.Add(-12*time.Hour).UnixMilli(), got[0].FromMS, "clipped to the view")
	assert.Equal(t, mon.UnixMilli(), got[0].ToMS)
	assert.Equal(t, mon.Add(workStartHour*time.Hour).UnixMilli(), got[1].ToMS)
	assert.Equal(t, mon.Add(workEndHour*time.Hour).UnixMilli(), got[2].FromMS)
	assert.Equal(t, mon.Add((24+workStartHour)*time.Hour).UnixMilli(), got[3].ToMS, "Tuesday's morning ends before the view does")
	for _, band := range got {
		assert.Equal(t, "out of hours", band.Label)
	}
}

func indexOfKey(keys []string, k string) int {
	for i, x := range keys {
		if x == k {
			return i
		}
	}
	return -1
}

// get_view carries the window's face: live or fixed, the window Back
// returns to, and the context extent as last read.
func TestGetViewCarriesTheNavigation(t *testing.T) {
	inst, _ := opsFixture(t)
	inst.cache.ctxFrom, inst.cache.ctxTo = t0.Add(-5*time.Hour), t0.Add(5*time.Hour)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.False(t, v.Live)
	assert.Equal(t, (Window{Last: time.Hour}).String(), v.PreviousRange, "the fixture replaced the default window")
	assert.Equal(t, t0.Add(-5*time.Hour), v.ContextFrom)
	assert.Equal(t, t0.Add(5*time.Hour), v.ContextTo)
	require.NoError(t, command(t, inst, opSetRange, RangeArgs{Last: "15m"}))
	v, err = query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.True(t, v.Live)
	assert.Equal(t, (Window{From: t0.Add(-time.Hour), To: t0.Add(time.Hour)}).String(), v.PreviousRange)
}
