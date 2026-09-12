package landoverlay

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

func crosses(a, b, c, d portolan.Point) bool {
	o := func(p, q, r portolan.Point) int {
		switch v := (q.Y-p.Y)*(r.X-q.X) - (q.X-p.X)*(r.Y-q.Y); {
		case v > 0:
			return 1
		case v < 0:
			return -1
		}
		return 0
	}
	o1, o2, o3, o4 := o(a, b, c), o(a, b, d), o(c, d, a), o(c, d, b)
	return o1 != o2 && o3 != o4 && o1 != 0 && o2 != 0 && o3 != 0 && o4 != 0
}

func selfIntersects(p []portolan.Point) bool {
	n := len(p)
	for i := range n {
		for j := i + 2; j < n; j++ {
			if i == 0 && j == n-1 {
				continue
			}
			if crosses(p[i], p[(i+1)%n], p[j], p[(j+1)%n]) {
				return true
			}
		}
	}
	return false
}

// Douglas–Peucker does not preserve simplicity: dropping vertices from a
// simple polygon can make its edges cross, and an ear clipper has no defined
// answer for one that does. Which vertices survive depends on the view, so
// such a ring is fine in one frame and not the next — the shape of the
// flicker. This is why the concave fill path hands its ring over whole.
func TestSimplifyingARingCanMakeItCrossItself(t *testing.T) {
	a, err := worldmap.LoadAtlas()
	require.NoError(t, err)

	worseAt := 0
	for _, z := range []float64{0, 1, 2} {
		v := portolan.NewView(portolan.ViewOptions{})
		v.SetSize(portolan.Point{X: 720, Y: 460})
		v.SetView(portolan.LL(20, 8), z)

		var lats, lngs []float64
		raw, simplified := 0, 0
		for i := range a.Countries {
			cy := &a.Countries[i]
			for r := range cy.RingCount() {
				lats, lngs = lats[:0], lngs[:0]
				lats, lngs, _ = cy.Ring(r, lats, lngs)
				if len(lats) < 3 {
					continue
				}
				pts := make([]portolan.Point, len(lats))
				for j := range lats {
					pts[j] = v.LatLngToContainerPoint(portolan.LL(lats[j], lngs[j]))
				}
				if selfIntersects(pts) {
					raw++
				}
				sp := portolan.Simplify(pts, 1.0)
				if len(sp) >= 3 && selfIntersects(sp) {
					simplified++
				}
			}
		}
		t.Logf("zoom %v: %d rings cross themselves as projected, %d after Simplify", z, raw, simplified)
		if simplified > raw {
			worseAt++
		}
	}
	require.Positive(t, worseAt, "simplification makes it worse at some zoom, which is why the fill path skips it")
}
