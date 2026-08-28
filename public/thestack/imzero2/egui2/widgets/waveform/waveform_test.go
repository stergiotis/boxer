package waveform

import (
	"math"
	"testing"

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
