package landoverlay

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// paintDegrees is the overlay as it drew before the atlas was kept in the
// projection plane: every ring read back as degrees, shifted by whole turns
// for a world copy, and projected in the frame. It is the oracle the plane
// path is held to.
func paintDegrees(p portolan.Projector, atlas *worldmap.Atlas, st Style) {
	b := p.View().Bounds()
	south, west, north, east := b.GetSouth(), b.GetWest(), b.GetNorth(), b.GetEast()
	var lats, lngs []float64
	for _, shift := range worldShifts(west, east) {
		for i := range atlas.Countries {
			cy := &atlas.Countries[i]
			if !meets(cy, south, west-shift, north, east-shift) {
				continue
			}
			for r := range cy.RingCount() {
				var hole bool
				lats, lngs, hole = cy.Ring(r, lats[:0], lngs[:0])
				if len(lats) < 3 {
					continue
				}
				for j := range lngs {
					lngs[j] += shift
				}
				if hole || st.NoFill {
					p.Polyline(lats, lngs, st.Border, st.BorderWidth)
					continue
				}
				p.Polygon(lats, lngs, st.Land, st.Border, st.BorderWidth)
			}
		}
	}
}

// The plane path must emit the paint stream the per-frame projection emits,
// byte for byte: the fill's triangulation depends on exact vertices, so "the
// same to a pixel" is the only equality worth having. The views include the
// trial's four and two that cross the antimeridian, where a world copy is a
// shift in the plane rather than in degrees.
func TestPlanePathPaintsWhatTheDegreePathPaints(t *testing.T) {
	level := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	defer zerolog.SetGlobalLevel(level)
	a := atlas(t)
	sum, zero, reset := scenetest.InstallHashing()
	t.Cleanup(reset)

	views := append(benchViews[:len(benchViews):len(benchViews)],
		struct {
			name      string
			lat, lng  float64
			zoom      float64
			w, h      float32
			wantsFill bool
		}{"pacific-z2", 0, 180, 2, 960, 600, true},
		struct {
			name      string
			lat, lng  float64
			zoom      float64
			w, h      float32
			wantsFill bool
		}{"bering-z4", 63, -178, 4, 720, 460, true})
	for _, v := range views {
		for _, st := range []Style{DefaultStyle(), {NoFill: true, Land: DefaultStyle().Land, Border: DefaultStyle().Border, BorderWidth: 1}} {
			frame := func(paint func(portolan.Projector)) uint64 {
				m := portolan.New(c.NewWidgetIdStack(), "", portolan.Options{NoTiles: true, Center: portolan.LL(v.lat, v.lng), Zoom: v.zoom})
				zero()
				m.Render(v.w, v.h, paint)
				return sum()
			}
			layer := &Layer{}
			want := frame(func(p portolan.Projector) { paintDegrees(p, a, st) })
			got := frame(func(p portolan.Projector) { layer.Paint(p, a, st) })
			require.Equal(t, want, got, "%s, NoFill=%v", v.name, st.NoFill)
			require.Positive(t, layer.Drawn(), "%s draws something", v.name)
			// A second frame reuses the plane and the scratch and must not drift.
			require.Equal(t, want, frame(func(p portolan.Projector) { layer.Paint(p, a, st) }), "%s, second frame", v.name)
		}
	}
}
