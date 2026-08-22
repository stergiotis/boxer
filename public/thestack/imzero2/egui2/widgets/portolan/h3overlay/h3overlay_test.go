package h3overlay

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/science/geo/h3"
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
	defer rt.Close()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	center, _, err := h.LatLngToCellE(ctx, h3.ResolutionR7, 51.0992, 17.0366)
	require.NoError(t, err)
	disk, _, err := h.GridDiskE(ctx, 1, center)
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
