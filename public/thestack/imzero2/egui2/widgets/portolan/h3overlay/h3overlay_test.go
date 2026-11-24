package h3overlay

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/science/geo/h3"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolutionForZoom(t *testing.T) {
	for _, c := range []struct {
		zoom float64
		res  h3.ResolutionE
	}{{0, 1}, {2, 1}, {4, 1}, {6, 2}, {10, 4}, {12, 5}, {14, 6}, {18, 8}, {26, 12}, {40, 12}} {
		assert.Equal(t, c.res, ResolutionForZoom(c.zoom), "zoom %v", c.zoom)
	}
}

func TestRingHelpers(t *testing.T) {
	// A unit square, counter-clockwise: area 1, centroid (0.5, 0.5).
	lats := []float64{0, 0, 1, 1}
	lngs := []float64{0, 1, 1, 0}
	assert.InDelta(t, 1.0, shoelace(lats, lngs), 1e-12)
	c := centroid(lats, lngs)
	assert.InDelta(t, 0.5, c.Lat, 1e-12)
	assert.InDelta(t, 0.5, c.Lng, 1e-12)
	// polygon offsets [0 2 3]: rings 0 and 2 open polygons, ring 1 is a hole.
	assert.True(t, isExterior(0, []int32{0, 2, 3}))
	assert.False(t, isExterior(1, []int32{0, 2, 3}))
	assert.True(t, isExterior(2, []int32{0, 2, 3}))
}

// The region's dissolve against the wasm bridge: a ring of six cells around
// a centre (the centre left out) dissolves to one polygon with an exterior
// and a hole; both rings come back closed; the label sits on the exterior;
// the same cells again hit the cache.
func TestRegionDissolve(t *testing.T) {
	ctx := context.Background()
	rt, err := h3.NewRuntime(ctx, h3.RuntimeConfig{PoolSize: 1})
	require.NoError(t, err)
	defer func() { _ = rt.Close() }()
	h, err := rt.Acquire(ctx)
	require.NoError(t, err)
	defer h.Release()

	center, _, err := h.LatLngToCell(ctx, h3.ResolutionR7, 51.0992, 17.0366)
	require.NoError(t, err)
	disk, _, err := h.GridDisk(ctx, 1, center)
	require.NoError(t, err)
	require.Len(t, disk, 7)
	ring := make([]uint64, 0, 6)
	for _, c := range disk {
		if c != center {
			ring = append(ring, c)
		}
	}
	require.Len(t, ring, 6)

	var r Region
	require.NoError(t, r.dissolve(ctx, h, ring))
	require.Len(t, r.ringLats, 2, "exterior + hole")
	for i := range r.ringLats {
		n := len(r.ringLats[i])
		assert.GreaterOrEqual(t, n, 7)
		assert.Equal(t, r.ringLats[i][0], r.ringLats[i][n-1], "ring %d closed", i)
		assert.Equal(t, r.ringLngs[i][0], r.ringLngs[i][n-1], "ring %d closed", i)
	}
	assert.True(t, r.haveLabel)
	// The label is near the centre cell — inside the hole, where the
	// exterior ring's vertex mean lands for a symmetric ring.
	assert.InDelta(t, 51.0992, r.labelAt.Lat, 0.02)
	assert.InDelta(t, 17.0366, r.labelAt.Lng, 0.03)

	key := r.key
	before := r.ringLats
	require.NoError(t, r.dissolve(ctx, h, ring))
	assert.Equal(t, key, r.key)
	assert.Equal(t, len(before), len(r.ringLats), "cached: nothing recomputed")

	// A different set recomputes: one cell → one closed ring of 7.
	require.NoError(t, r.dissolve(ctx, h, []uint64{center}))
	assert.NotEqual(t, key, r.key)
	require.Len(t, r.ringLats, 1)
	assert.Len(t, r.ringLats[0], 7)
}

func TestWrapLngAndUnwrap(t *testing.T) {
	for _, c := range []struct{ in, out float64 }{{0, 0}, {180, 180}, {-180, -180}, {190, -170}, {-190, 170}, {540, -180}, {365, 5}} {
		assert.InDelta(t, c.out, wrapLng(c.in), 1e-9, "%v", c.in)
	}
	// A cell across the 180th meridian has corners at both ends of the
	// longitudes; unwrapped, they are continuous at one end or the other.
	lngs := []float64{179.2, -179.6, -179.1, 179.7}
	assert.True(t, CrossesAntimeridian(lngs))
	assert.False(t, CrossesAntimeridian([]float64{8.1, 8.4, 8.2}))
	assert.False(t, CrossesAntimeridian(nil))
	assert.InDeltaSlice(t, []float64{179.2, 180.4, 180.9, 179.7}, unwrap(nil, lngs, 360), 1e-9)
	assert.InDeltaSlice(t, []float64{-180.8, -179.6, -179.1, -180.3}, unwrap(nil, lngs, -360), 1e-9)
}

// A view is asked of H3 in slices: one box wider than half a turn comes back
// as its complement, the cells of the far side of the globe. Seen on a map at
// world view, whose grid showed at the left and right edges only.
func TestViewportCellsOfAWideView(t *testing.T) {
	ctx := context.Background()
	rt, err := h3.NewRuntime(ctx, h3.RuntimeConfig{PoolSize: 1})
	require.NoError(t, err)
	defer func() { _ = rt.Close() }()
	h, err := rt.Acquire(ctx)
	require.NoError(t, err)
	defer h.Release()

	cell := func(lat, lng float64) uint64 {
		id, status, cErr := h.LatLngToCell(ctx, h3.ResolutionR1, lat, lng)
		require.NoError(t, cErr)
		require.Equal(t, h3.StatusOk, status)
		return id
	}
	box := func(s, w, n, e float64) portolan.LatLngBounds {
		return portolan.LatLngBounds{}.Extend(portolan.LL(s, w)).Extend(portolan.LL(n, e))
	}
	zurich, tokyo, fiji, lima := cell(47.37, 8.54), cell(35.68, 139.69), cell(-16.85, 179.99), cell(-12.05, -77.04)

	// The world but for a strip at the antimeridian: the middle is in it.
	world, err := ViewportCells(ctx, h, box(-60, -170, 70, 170), h3.ResolutionR1)
	require.NoError(t, err)
	assert.Contains(t, world, zurich)
	assert.Contains(t, world, tokyo)
	assert.Contains(t, world, lima)
	once := map[uint64]struct{}{}
	for _, id := range world {
		once[id] = struct{}{}
	}
	assert.Len(t, once, len(world), "a cell on a slice's edge comes back once")

	// A view panned across the 180th meridian: its longitudes run past 180.
	pacific, err := ViewportCells(ctx, h, box(-40, 150, 40, 210), h3.ResolutionR1)
	require.NoError(t, err)
	assert.Contains(t, pacific, fiji)
	assert.NotContains(t, pacific, zurich)
	assert.NotContains(t, pacific, lima)

	// A narrow view is one request, as before.
	europe, err := ViewportCells(ctx, h, box(40, 0, 55, 20), h3.ResolutionR1)
	require.NoError(t, err)
	assert.Contains(t, europe, zurich)
	assert.NotContains(t, europe, tokyo)
}
