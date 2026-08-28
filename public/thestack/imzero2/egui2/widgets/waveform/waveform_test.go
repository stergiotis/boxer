package waveform

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestViewFitAllAndClamp(t *testing.T) {
	v := fitAll(48000, 1200)
	require.Equal(t, 0.0, v.FromFrame)
	require.InDelta(t, 40.0, v.FramesPerPx, 1e-9)

	// Zooming out past fit-all clamps to fit-all; the span stays inside.
	out := v.zoomAt(600, 0.5, 48000, 1200)
	require.InDelta(t, 40.0, out.FramesPerPx, 1e-9)
	require.Equal(t, 0.0, out.FromFrame)

	// Zooming in keeps the anchored frame under the anchor.
	in := v.zoomAt(300, 4, 48000, 1200)
	require.InDelta(t, 10.0, in.FramesPerPx, 1e-9)
	require.InDelta(t, v.XToFrame(300), in.XToFrame(300), 1e-6)

	// Panning past either end clamps.
	require.Equal(t, 0.0, in.panPx(1e6, 48000, 1200).FromFrame)
	require.InDelta(t, 48000-1200*10.0, in.panPx(-1e6, 48000, 1200).FromFrame, 1e-9)
}

func TestViewInvariantsUnderRandomGestures(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		frames := rapid.Int64Range(0, 1<<31).Draw(t, "frames")
		w := rapid.Float32Range(1, 4000).Draw(t, "w")
		v := fitAll(frames, w)
		fit := v.FramesPerPx
		n := rapid.IntRange(1, 40).Draw(t, "n")
		for range n {
			switch rapid.IntRange(0, 2).Draw(t, "op") {
			case 0:
				v = v.zoomAt(rapid.Float32Range(0, w).Draw(t, "ax"), rapid.Float64Range(0.1, 10).Draw(t, "f"), frames, w)
			case 1:
				v = v.panPx(rapid.Float32Range(-5000, 5000).Draw(t, "dx"), frames, w)
			default:
				v = View{FromFrame: rapid.Float64Range(-1e9, 1e9).Draw(t, "from"), FramesPerPx: rapid.Float64Range(-1, 1e7).Draw(t, "fpp")}.clamp(frames, w)
			}
			require.False(t, math.IsNaN(v.FromFrame) || math.IsNaN(v.FramesPerPx))
			require.GreaterOrEqual(t, v.FramesPerPx, minFramesPerPx-1e-12)
			require.LessOrEqual(t, v.FramesPerPx, fit+1e-9)
			require.GreaterOrEqual(t, v.FromFrame, 0.0)
			span := float64(w) * v.FramesPerPx
			if span < float64(frames) {
				require.LessOrEqual(t, v.FromFrame+span, float64(frames)+1e-6)
			} else {
				require.Equal(t, 0.0, v.FromFrame)
			}
		}
	})
}

func TestViewMapsRoundTrip(t *testing.T) {
	v := View{FromFrame: 1234.5, FramesPerPx: 3.25}
	for _, x := range []float32{0, 1, 77.5, 1199} {
		require.InDelta(t, float64(x), float64(v.FrameToX(v.XToFrame(x))), 1e-3)
	}
	require.Equal(t, int64(1234), v.FrameAtX(0))
	require.Equal(t, int64(1237), v.FrameAtX(1))
}

func TestPickDurationStep(t *testing.T) {
	// 20 s over 1200 px at 90 px/tick → 2 s ticks (1 s would be 60 px).
	require.Equal(t, 2*time.Second, pickDurationStep(20*time.Second, 1200, 90))
	// 12 h over 1200 px → one hour is 100 px, so 1 h.
	require.Equal(t, time.Hour, pickDurationStep(12*time.Hour, 1200, 90))
	// 5 ms over 1200 px → 500 µs ticks (120 px).
	require.Equal(t, 500*time.Microsecond, pickDurationStep(5*time.Millisecond, 1200, 90))
	// Degenerate spans do not panic.
	require.Equal(t, durationLadder[0], pickDurationStep(0, 1200, 90))
}

func TestDurationTicksAreMultiplesInsideTheSpan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		from := time.Duration(rapid.Int64Range(0, int64(13*time.Hour)).Draw(t, "from"))
		span := time.Duration(rapid.Int64Range(1, int64(time.Hour)).Draw(t, "span"))
		to := from + span
		step := pickDurationStep(span, 1200, 90)
		ticks := durationTicks(from, to, step, nil)
		if span >= 2*step {
			// A span narrower than a step legitimately holds no tick.
			require.NotEmpty(t, ticks)
		}
		for i, tk := range ticks {
			require.Zero(t, tk%step)
			require.GreaterOrEqual(t, tk, from)
			require.LessOrEqual(t, tk, to)
			if i > 0 {
				require.Equal(t, step, tk-ticks[i-1])
			}
		}
		// Spacing at or above the target means the count is bounded.
		require.LessOrEqual(t, len(ticks), 1200/90+2)
	})
}

func TestFormatOffsetGolden(t *testing.T) {
	cases := []struct {
		d    time.Duration
		step time.Duration
		want string
	}{
		{0, time.Second, "0:00"},
		{83*time.Second + 250*time.Millisecond, time.Second, "1:23"},
		{83*time.Second + 250*time.Millisecond, 100 * time.Millisecond, "1:23.250"},
		{83*time.Second + 250*time.Millisecond + 7*time.Microsecond, 5 * time.Microsecond, "1:23.250007"},
		{12*time.Hour + 5*time.Minute + 9*time.Second, time.Hour, "12:05:09"},
		{2*time.Second - time.Microsecond, time.Millisecond, "0:01.999"}, // truncated, not rounded
		{-1500 * time.Millisecond, 100 * time.Millisecond, "-0:01.500"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, formatOffset(tc.d, tc.step), "%v @ %v", tc.d, tc.step)
	}
}

func TestFormatClock(t *testing.T) {
	epoch := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	require.Equal(t, "09:30:05", formatClock(epoch.Add(5*time.Second), time.Second))
	require.Equal(t, "09:30:05.250", formatClock(epoch.Add(5250*time.Millisecond), 10*time.Millisecond))
}

func TestReduceColumnsBruteForce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		channels := rapid.IntRange(1, 3).Draw(t, "channels")
		frames := rapid.IntRange(0, 500).Draw(t, "frames")
		samples := rapid.SliceOfN(rapid.Float32Range(-1, 1), frames*channels, frames*channels).Draw(t, "samples")
		fpp := rapid.Float64Range(1, 50).Draw(t, "fpp")
		cols := rapid.IntRange(1, 64).Draw(t, "cols")
		ch := rapid.IntRange(0, channels-1).Draw(t, "ch")
		mins, maxs := make([]float32, cols), make([]float32, cols)
		filled := reduceColumns(samples, channels, ch, fpp, mins, maxs)
		for c := range cols {
			f0 := int(math.Floor(float64(c) * fpp))
			f1 := int(math.Floor(float64(c+1) * fpp))
			if f1 <= f0 {
				f1 = f0 + 1
			}
			if f0 >= frames {
				require.Less(t, filled, c+1)
				continue
			}
			require.GreaterOrEqual(t, filled, c+1)
			f1 = min(f1, frames)
			lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
			for f := f0; f < f1; f++ {
				s := samples[f*channels+ch]
				lo, hi = min(lo, s), max(hi, s)
			}
			require.Equal(t, lo, mins[c])
			require.Equal(t, hi, maxs[c])
		}
	})
}

func TestReduceColumnsRejectsSampleZoom(t *testing.T) {
	mins, maxs := make([]float32, 4), make([]float32, 4)
	require.Equal(t, 0, reduceColumns(make([]float32, 100), 1, 0, 0.5, mins, maxs))
}
