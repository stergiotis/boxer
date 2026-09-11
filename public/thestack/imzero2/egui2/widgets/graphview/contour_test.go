package graphview

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// maskIso is a binary iso source over a small grid, for the tracer tests:
// boundaries sit midway between samples and saddles never join.
type maskIso struct {
	w, h int32
	m    []bool
}

func (s maskIso) inside(i, j int32) bool {
	return i >= 0 && j >= 0 && i < s.w && j < s.h && s.m[j*s.w+i]
}
func (s maskIso) frac(_, _, _, _ int32) float32 { return 0.5 }
func (s maskIso) saddle(_, _ int32) bool        { return false }

func maskFrom(rows ...string) maskIso {
	s := maskIso{w: int32(len(rows[0])), h: int32(len(rows))}
	for _, r := range rows {
		for _, ch := range r {
			s.m = append(s.m, ch == '#')
		}
	}
	return s
}

func traceAll(t *testing.T, s maskIso) rings {
	t.Helper()
	var tr tracer
	var out rings
	tr.setup(s.w, s.h)
	tr.trace(s, 1, 0, 0, s.w-1, s.h-1, &out)
	return out
}

func TestTracerSingleSampleIsADiamond(t *testing.T) {
	out := traceAll(t, maskFrom(
		"...",
		".#.",
		"...",
	))
	require.Equal(t, 1, out.count())
	xs, ys := out.ring(0)
	require.Len(t, xs, 4)
	require.InDelta(t, -0.5, signedArea(xs, ys), 1e-6, "a unit diamond, wound as an outer ring")
	for i := range xs {
		require.InDelta(t, 1.5, xs[i], 0.5)
		require.InDelta(t, 1.5, ys[i], 0.5)
	}
}

func TestTracerBlockAndHoleWindOppositely(t *testing.T) {
	out := traceAll(t, maskFrom(
		".....",
		".###.",
		".#.#.",
		".###.",
		".....",
	))
	require.Equal(t, 2, out.count())
	var outer, hole int
	for r := range out.count() {
		xs, ys := out.ring(r)
		if signedArea(xs, ys) < 0 {
			outer++
		} else {
			hole++
		}
	}
	require.Equal(t, 1, outer)
	require.Equal(t, 1, hole)
}

func TestTracerSeparatesIslandsAndSaddles(t *testing.T) {
	out := traceAll(t, maskFrom(
		"#..#",
		"....",
		"#..#",
	))
	require.Equal(t, 4, out.count(), "four corner islands")
	out = traceAll(t, maskFrom(
		"#.",
		".#",
	))
	require.Equal(t, 2, out.count(), "a saddle the source keeps apart is two rings")
	// The grid edge closes rings for samples on the border.
	for r := range out.count() {
		xs, _ := out.ring(r)
		require.GreaterOrEqual(t, len(xs), 3)
	}
}

func TestTracerReuseResetsEdgeIndex(t *testing.T) {
	var tr tracer
	s := maskFrom("...", ".#.", "...")
	var a, b rings
	tr.setup(s.w, s.h)
	tr.trace(s, 1, 0, 0, s.w-1, s.h-1, &a)
	tr.setup(s.w, s.h)
	tr.trace(s, 1, 0, 0, s.w-1, s.h-1, &b)
	require.Equal(t, a.xs, b.xs)
	require.Equal(t, a.ys, b.ys)
}

func TestChaikinDoublesAndRounds(t *testing.T) {
	xs := []float32{0, 10, 10, 0}
	ys := []float32{0, 0, 10, 10}
	var out rings
	sx, sy := appendSmoothed(xs, ys, &out, nil, nil)
	require.NotNil(t, sx)
	require.NotNil(t, sy)
	require.Equal(t, 1, out.count())
	rx, ry := out.ring(0)
	require.Len(t, rx, 16, "two passes over four points")
	for i := range rx {
		require.False(t, rx[i] == 0 && ry[i] == 0, "the corner itself is cut off")
	}
	require.InDelta(t, signedArea(xs, ys), signedArea(rx, ry), 100*0.2, "the area shrinks a little, the winding holds")
}

func TestRingTooSmall(t *testing.T) {
	require.True(t, ringTooSmall(0.4, 1))
	require.False(t, ringTooSmall(-0.6, 1))
	require.True(t, ringTooSmall(-30, 8))
}
