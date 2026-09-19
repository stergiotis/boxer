package vectorfield_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

func linearWindow() vectorfield.Window {
	win := vectorfield.Window{West: 170, North: 10, DLon: 2, DLat: 1, Cols: 12, Rows: 8}
	n := win.Cols * win.Rows
	win.U, win.V, win.Speed = make([]float32, n), make([]float32, n), make([]float32, n)
	for r := range win.Rows {
		for c := range win.Cols {
			i := r*win.Cols + c
			win.U[i] = float32(0.5*win.LonAt(c) - 2*win.LatAt(r))
			win.V[i] = float32(3*win.LatAt(r) + 1)
			win.Speed[i] = float32(win.LonAt(c))
		}
	}
	return win
}

// Bilinear interpolation reproduces a field linear in position exactly, and
// the window's longitudes run on past 180 without a fold.
func TestSampleIsExactOnALinearField(t *testing.T) {
	win := linearWindow()
	rapid.Check(t, func(t *rapid.T) {
		lon := rapid.Float64Range(win.West, win.East()).Draw(t, "lon")
		lat := rapid.Float64Range(win.South(), win.North).Draw(t, "lat")
		u, v, s, ok := win.Sample(lon, lat)
		require.True(t, ok)
		require.InDelta(t, 0.5*lon-2*lat, u, 1e-3)
		require.InDelta(t, 3*lat+1, v, 1e-3)
		require.InDelta(t, lon, s, 1e-3)
	})
}

func TestSampleIsStrict(t *testing.T) {
	win := linearWindow()
	nan := float32(math.NaN())
	hole := 3*win.Cols + 5
	win.U[hole], win.V[hole], win.Speed[hole] = nan, nan, nan

	_, _, _, ok := win.Sample(win.LonAt(5)+0.1, win.LatAt(3)-0.1)
	require.False(t, ok, "a missing corner is a missing value, never an estimate from three")
	_, _, _, ok = win.Sample(win.LonAt(4)+0.1, win.LatAt(2)-0.9)
	require.False(t, ok)
	_, _, _, ok = win.Sample(win.LonAt(7)+0.1, win.LatAt(3)-0.1)
	require.True(t, ok)

	for _, p := range [][2]float64{{win.West - 0.01, 5}, {win.East() + 0.01, 5}, {180, win.North + 0.01}, {180, win.South() - 0.01}, {math.NaN(), 5}, {180, math.NaN()}} {
		_, _, _, ok = win.Sample(p[0], p[1])
		require.False(t, ok, "outside the window: %v", p)
		require.False(t, win.Covers(p[0], p[1]))
	}
	_, _, _, ok = win.Sample(win.East(), win.South())
	require.True(t, ok, "the last node is inside")

	empty := vectorfield.Window{}
	_, _, _, ok = empty.Sample(0, 0)
	require.False(t, ok)
}
