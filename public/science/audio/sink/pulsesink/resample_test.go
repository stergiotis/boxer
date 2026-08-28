package pulsesink

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestResampleLinearRateOneIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		channels := rapid.IntRange(1, 2).Draw(t, "channels")
		frames := rapid.IntRange(2, 200).Draw(t, "frames")
		src := rapid.SliceOfN(rapid.Float32Range(-1, 1), frames*channels, frames*channels).Draw(t, "src")
		out := make([]float32, (frames-1)*channels)
		n, consumed, frac := resampleLinear(src, channels, 0, 1, out)
		require.Equal(t, frames-1, n, "the last frame has no neighbour to interpolate toward")
		require.Equal(t, int64(frames-1), consumed)
		require.Equal(t, 0.0, frac)
		require.Equal(t, src[:(frames-1)*channels], out[:n*channels])
	})
}

func TestResampleLinearConstantSignalStaysConstant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		channels := rapid.IntRange(1, 2).Draw(t, "channels")
		frames := rapid.IntRange(4, 300).Draw(t, "frames")
		level := rapid.Float32Range(-1, 1).Draw(t, "level")
		rate := rapid.Float64Range(0.3, 4).Draw(t, "rate")
		frac := rapid.Float64Range(0, 0.999).Draw(t, "frac")
		src := make([]float32, frames*channels)
		for i := range src {
			src[i] = level
		}
		out := make([]float32, 1000*channels)
		n, consumed, nextFrac := resampleLinear(src, channels, frac, rate, out)
		require.Positive(t, n)
		for _, v := range out[:n*channels] {
			require.InDelta(t, level, v, 1e-5)
		}
		// Consumption tracks the ratio: the total advance is frac + n·rate.
		require.InDelta(t, frac+float64(n)*rate, float64(consumed)+nextFrac, 1e-9)
		require.GreaterOrEqual(t, nextFrac, 0.0)
		require.Less(t, nextFrac, 1.0)
		// Never read past the end.
		require.LessOrEqual(t, framesNeeded(n, frac, rate), int64(frames))
	})
}

func TestResampleLinearChunkingIsContinuous(t *testing.T) {
	// A ramp resampled in two chunks equals the same ramp resampled at once.
	const frames, channels = 400, 1
	src := make([]float32, frames)
	for i := range src {
		src[i] = float32(i)
	}
	rate := 1.37
	whole := make([]float32, 200)
	n1, _, _ := resampleLinear(src, channels, 0, rate, whole)
	require.Equal(t, 200, n1)

	a := make([]float32, 77)
	na, consumedA, fracA := resampleLinear(src, channels, 0, rate, a)
	require.Equal(t, 77, na)
	b := make([]float32, 123)
	nb, _, _ := resampleLinear(src[consumedA:], channels, fracA, rate, b)
	require.Equal(t, 123, nb)
	got := append(a[:na], b[:nb]...)
	for i := range got {
		require.InDelta(t, whole[i], got[i], 1e-3, "sample %d", i)
	}
	// And the ramp is reproduced: output i ≈ i·rate.
	for i, v := range got {
		require.InDelta(t, float64(i)*rate, float64(v), 1e-2)
	}
}

func TestFramesNeeded(t *testing.T) {
	require.Equal(t, int64(0), framesNeeded(0, 0, 1))
	require.Equal(t, int64(2), framesNeeded(1, 0, 1))
	require.Equal(t, int64(11), framesNeeded(10, 0, 1))
	require.Equal(t, int64(20), framesNeeded(10, 0.5, 2)) // last pos 18.5 → floor 18 → +2
	require.Equal(t, int64(int64(math.Floor(0.9+9*0.5))+2), framesNeeded(10, 0.9, 0.5))
}
