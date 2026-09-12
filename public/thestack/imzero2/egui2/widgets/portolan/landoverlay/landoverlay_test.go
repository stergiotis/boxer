package landoverlay

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

func atlas(t *testing.T) *worldmap.Atlas {
	t.Helper()
	a, err := worldmap.LoadAtlas()
	require.NoError(t, err)
	return a
}

func country(t *testing.T, a *worldmap.Atlas, name string) *worldmap.Country {
	t.Helper()
	idx, ok := a.Resolve(name)
	require.True(t, ok, "atlas resolves %q", name)
	return &a.Countries[idx]
}

func TestCullKeepsWhatMeetsTheViewportAndDropsTheRest(t *testing.T) {
	a := atlas(t)
	ch := country(t, a, "Switzerland")

	require.True(t, meets(ch, 45.5, 5.5, 48.0, 10.5))
	require.False(t, meets(ch, -20, -150, -10, -140))

	// Touching counts: the cull must not drop a country whose edge is the
	// viewport's, or a coastline would vanish at the frame edge.
	minLat, minLng, maxLat, maxLng, ok := ch.GeoBounds()
	require.True(t, ok)
	require.True(t, meets(ch, maxLat, minLng, maxLat+1, maxLng), "north edge")
	require.True(t, meets(ch, minLat-1, maxLng, minLat, maxLng+1), "east edge")

	kept := 0
	for i := range a.Countries {
		if meets(&a.Countries[i], -90, -180, 90, 180) {
			kept++
		}
	}
	require.Equal(t, len(a.Countries), kept, "a whole-world box keeps everything")
}

func TestSwitzerlandsBoundsAreWhereSwitzerlandIs(t *testing.T) {
	minLat, minLng, maxLat, maxLng, ok := country(t, atlas(t), "Switzerland").GeoBounds()
	require.True(t, ok)
	require.InDelta(t, 45.8, minLat, 0.6)
	require.InDelta(t, 47.8, maxLat, 0.6)
	require.InDelta(t, 6.0, minLng, 0.6)
	require.InDelta(t, 10.5, maxLng, 0.6)
}

func TestRingsComeBackAsDegreesAndReuseTheBuffer(t *testing.T) {
	ch := country(t, atlas(t), "Switzerland")
	require.Positive(t, ch.RingCount())

	var lats, lngs []float64
	lats, lngs, hole := ch.Ring(0, lats, lngs)
	require.Equal(t, len(lats), len(lngs))
	require.GreaterOrEqual(t, len(lats), 3)
	require.False(t, hole, "Switzerland's first ring is an outer boundary")
	for i := range lats {
		require.GreaterOrEqual(t, lats[i], -90.0)
		require.LessOrEqual(t, lats[i], 90.0)
		require.GreaterOrEqual(t, lngs[i], -180.0)
		require.LessOrEqual(t, lngs[i], 180.0)
	}

	capBefore := cap(lats)
	lats, lngs = lats[:0], lngs[:0]
	lats, lngs, _ = ch.Ring(0, lats, lngs)
	require.Equal(t, capBefore, cap(lats), "no reallocation on the second frame")

	l2, g2, h2 := ch.Ring(ch.RingCount(), lats, lngs)
	require.Equal(t, len(lats), len(l2))
	require.Equal(t, len(lngs), len(g2))
	require.False(t, h2)
}

func TestTheAssetsOneHoleIsReportedAsOne(t *testing.T) {
	// The atlas comment names exactly one interior ring in the vendored
	// asset — South Africa's Lesotho enclave. A filled overlay must know.
	a := atlas(t)
	holes := 0
	var lats, lngs []float64
	for i := range a.Countries {
		cy := &a.Countries[i]
		for r := range cy.RingCount() {
			lats, lngs = lats[:0], lngs[:0]
			var hole bool
			lats, lngs, hole = cy.Ring(r, lats, lngs)
			if hole {
				holes++
			}
		}
	}
	require.Equal(t, 1, holes)
}

// Rings are drawn as the atlas holds them, and this is why nothing here
// "corrects" a longitude. Exactly one ring in the asset steps a full turn
// between consecutive vertices, and that step is not a crossing: it is
// Antarctica's seam over the pole, down the 180th meridian to latitude −90,
// across, and back up the −180th. Making such a ring's longitudes continuous
// — which is what an antimeridian fix does — moves almost every vertex it has
// and puts the continent a world width east of everything else.
func TestTheOnlyFullTurnInTheAssetIsAPoleSeamAndNotACrossing(t *testing.T) {
	a := atlas(t)
	var lats, lngs []float64
	found := 0
	for i := range a.Countries {
		cy := &a.Countries[i]
		for r := range cy.RingCount() {
			lats, lngs = lats[:0], lngs[:0]
			lats, lngs, _ = cy.Ring(r, lats, lngs)
			if len(lngs) < 2 {
				continue
			}
			j := bigStep(lngs)
			if j < 0 {
				continue
			}
			found++
			require.Equal(t, "Antarctica", cy.Name)
			// A seam, not a journey: the step is exactly a full turn, both
			// ends sit on the meridian, and the latitude does not move.
			require.Equal(t, 360.0, math.Abs(lngs[j]-lngs[j-1]))
			require.Equal(t, 180.0, math.Abs(lngs[j-1]))
			require.Equal(t, 180.0, math.Abs(lngs[j]))
			require.Equal(t, lats[j-1], lats[j])
			require.Equal(t, -90.0, lats[j], "and it runs across the pole")
		}
	}
	require.Equal(t, 1, found, "one such ring, and it is the seam")
}

// bigStep returns the index of the first vertex more than half a turn from
// its predecessor, or -1.
func bigStep(lngs []float64) int {
	for i := 1; i < len(lngs); i++ {
		if math.Abs(lngs[i]-lngs[i-1]) > 180 {
			return i
		}
	}
	return -1
}
