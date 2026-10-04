package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// A point projected to the plane once and taken to the canvas each frame must
// land on the pixel the per-frame projection gives — to the bit, since the
// fill's triangulation depends on exact vertex positions and a layer mixing
// the two paths would otherwise shimmer at the seams.
func TestPlaneToCanvasIsToCanvasWithTheProjectionDone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		zoom := rapid.Float64Range(0, 19).Draw(t, "zoom")
		v := NewView(ViewOptions{})
		v.SetSize(Point{X: rapid.Float64Range(64, 2048).Draw(t, "w"), Y: rapid.Float64Range(64, 2048).Draw(t, "h")})
		v.SetView(LL(rapid.Float64Range(-80, 80).Draw(t, "clat"), rapid.Float64Range(-180, 180).Draw(t, "clng")), zoom)
		p := Projector{view: v}
		ll := LL(rapid.Float64Range(-89, 89).Draw(t, "lat"), rapid.Float64Range(-540, 540).Draw(t, "lng"))
		want := p.ToCanvas(ll)
		got := p.PlaneToCanvas(v.CRS().Project(ll))
		require.Equal(t, math.Float64bits(want.X), math.Float64bits(got.X), "x")
		require.Equal(t, math.Float64bits(want.Y), math.Float64bits(got.Y), "y")
	})
}

// The overlays simplify thousands of lines a frame through one reused
// scratch; what it returns must not depend on what it held before.
func TestSimplifyScratchReuseMatchesSimplify(t *testing.T) {
	var sc simplifyScratch
	var dst []Point
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 200).Draw(t, "n")
		pts := make([]Point, n)
		for i := range pts {
			pts[i] = Pt(rapid.Float64Range(-500, 500).Draw(t, "x"), rapid.Float64Range(-500, 500).Draw(t, "y"))
		}
		tol := rapid.Float64Range(0.01, 20).Draw(t, "tol")
		dst = sc.simplify(dst[:0], pts, tol)
		require.Equal(t, Simplify(pts, tol), dst)
	})
}
