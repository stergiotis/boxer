package h3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type dissolveRecord struct {
	Name           string    `json:"name"`
	Res            uint8     `json:"res"`
	Cells          []uint64  `json:"cells"`
	Lats           []float64 `json:"lats"`
	Lngs           []float64 `json:"lngs"`
	RingOffsets    []int32   `json:"ring_offsets"`
	PolygonOffsets []int32   `json:"polygon_offsets"`
}

func TestDissolve_Golden(t *testing.T) {
	recs := readNDJSON[dissolveRecord](t, "golden_dissolve.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	for _, r := range recs {
		lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, r.Cells)
		require.NoError(t, err, "name=%s", r.Name)
		require.Equal(t, r.RingOffsets, ringOffsets, "name=%s", r.Name)
		require.Equal(t, r.PolygonOffsets, polygonOffsets, "name=%s", r.Name)
		require.Len(t, lats, len(r.Lats), "name=%s", r.Name)
		require.Len(t, lngs, len(r.Lngs), "name=%s", r.Name)
		for j, wantLat := range r.Lats {
			require.InDelta(t, wantLat, lats[j], 1e-9, "name=%s vertex=%d", r.Name, j)
			require.InDelta(t, r.Lngs[j], lngs[j], 1e-9, "name=%s vertex=%d", r.Name, j)
		}
	}
}

// dissolveFixture returns a class-II (even resolution, so no icosahedron
// distortion vertices) hexagon near San Francisco and its six neighbours.
func dissolveFixture(t *testing.T, ctx context.Context, h *Handle) (centre uint64, neighbours []uint64) {
	t.Helper()
	centre, status, err := h.LatLngToCellE(ctx, ResolutionR8, 37.7749, -122.4194)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	disk, status, err := h.GridDiskE(ctx, 1, centre)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	require.Len(t, disk, 7)
	for _, c := range disk {
		if c != centre {
			neighbours = append(neighbours, c)
		}
	}
	require.Len(t, neighbours, 6)
	return
}

// requireDissolveShape checks the two-level CSR invariants and that every
// dissolved vertex coincides (within 1e-9 deg) with a vertex of one of the
// input cells' boundaries.
func requireDissolveShape(t *testing.T, ctx context.Context, h *Handle, cells []uint64,
	lats, lngs []float64, ringOffsets, polygonOffsets []int32,
	wantPolygons int, wantRings int, wantVertices int,
) {
	t.Helper()
	require.Len(t, lats, wantVertices)
	require.Len(t, lngs, wantVertices)
	require.Len(t, polygonOffsets, wantPolygons+1)
	require.Len(t, ringOffsets, wantRings+1)
	requireCSRInvariants(t, polygonOffsets, wantPolygons, wantRings)
	requireCSRInvariants(t, ringOffsets, wantRings, wantVertices)

	bLats, bLngs, _, status, err := h.CellsToBoundariesE(ctx, cells, nil, nil, nil, nil)
	require.NoError(t, err)
	require.False(t, AnyFailure(status))
	for i := range lats {
		found := false
		for j := range bLats {
			if abs64(bLats[j]-lats[i]) <= 1e-9 && abs64(bLngs[j]-lngs[i]) <= 1e-9 {
				found = true
				break
			}
		}
		require.True(t, found, "vertex %d (%g, %g) is not a boundary vertex of any input cell", i, lats[i], lngs[i])
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// shoelace returns twice the signed planar area of an open ring in the
// (lng, lat) plane: positive for counter-clockwise, negative for clockwise.
// Adequate for orientation checks away from the poles and the antimeridian.
func shoelace(lats, lngs []float64) (sum float64) {
	n := len(lats)
	for i := range n {
		j := (i + 1) % n
		sum += lngs[i]*lats[j] - lngs[j]*lats[i]
	}
	return
}

func TestDissolve_SingleCell(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	centre, _ := dissolveFixture(t, ctx, h)
	cells := []uint64{centre}
	lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, cells)
	require.NoError(t, err)
	requireDissolveShape(t, ctx, h, cells, lats, lngs, ringOffsets, polygonOffsets, 1, 1, 6)
	require.Greater(t, shoelace(lats, lngs), 0.0, "exterior must wind counter-clockwise")
}

func TestDissolve_Disk(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	centre, neighbours := dissolveFixture(t, ctx, h)
	cells := append([]uint64{centre}, neighbours...)
	lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, cells)
	require.NoError(t, err)
	// Seven hexagons: every centre vertex is interior; each outer hexagon
	// keeps three vertices of its own on the outline.
	requireDissolveShape(t, ctx, h, cells, lats, lngs, ringOffsets, polygonOffsets, 1, 1, 18)
	require.Greater(t, shoelace(lats, lngs), 0.0, "exterior must wind counter-clockwise")
}

func TestDissolve_RingWithHole(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	_, neighbours := dissolveFixture(t, ctx, h)
	lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, neighbours)
	require.NoError(t, err)
	// One polygon: the 18-vertex outline plus the 6-vertex hole left by
	// the missing centre.
	requireDissolveShape(t, ctx, h, neighbours, lats, lngs, ringOffsets, polygonOffsets, 1, 2, 24)
	require.Equal(t, int32(18), ringOffsets[1]-ringOffsets[0], "exterior comes first")
	require.Equal(t, int32(6), ringOffsets[2]-ringOffsets[1], "hole comes second")
	exterior := shoelace(lats[ringOffsets[0]:ringOffsets[1]], lngs[ringOffsets[0]:ringOffsets[1]])
	hole := shoelace(lats[ringOffsets[1]:ringOffsets[2]], lngs[ringOffsets[1]:ringOffsets[2]])
	require.Greater(t, exterior, 0.0, "exterior must wind counter-clockwise")
	require.Less(t, hole, 0.0, "hole must wind clockwise")
}

func TestDissolve_TwoDisjointCells(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	centre, _ := dissolveFixture(t, ctx, h)
	far, status, err := h.LatLngToCellE(ctx, ResolutionR8, 48.8566, 2.3522)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	cells := []uint64{centre, far}
	lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, cells)
	require.NoError(t, err)
	requireDissolveShape(t, ctx, h, cells, lats, lngs, ringOffsets, polygonOffsets, 2, 2, 12)
	for p := range 2 {
		require.Equal(t, int32(1), polygonOffsets[p+1]-polygonOffsets[p], "polygon %d has one ring", p)
	}
}

func TestDissolve_Empty(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, lats)
	require.Empty(t, lngs)
	require.Equal(t, []int32{0}, ringOffsets)
	require.Equal(t, []int32{0}, polygonOffsets)
}

func TestDissolve_InvalidCell(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	centre, _ := dissolveFixture(t, ctx, h)
	_, _, _, _, err = h.DissolveE(ctx, []uint64{centre, 0xdeadbeef_cafebabe})
	require.ErrorIs(t, err, ErrDissolveInvalidCell)
}

func TestDissolve_MixedResolutionRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	centre, _ := dissolveFixture(t, ctx, h)
	coarse, status, err := h.LatLngToCellE(ctx, ResolutionR7, 48.8566, 2.3522)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	_, _, _, _, err = h.DissolveE(ctx, []uint64{centre, coarse})
	require.ErrorIs(t, err, ErrDissolveMixedResolution)
}

func TestDissolve_DuplicateRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	centre, _ := dissolveFixture(t, ctx, h)
	_, _, _, _, err = h.DissolveE(ctx, []uint64{centre, centre})
	require.ErrorIs(t, err, ErrDissolveDuplicateInput)
}

func TestDissolve_GrowProtocol(t *testing.T) {
	// A class-III cell (odd resolution) whose boundary crosses an
	// icosahedron edge carries a distortion vertex, so its outline has
	// more than six vertices and a single-cell dissolve exceeds the 6*n
	// initial vertex cap, exercising the retry. Res-1 cells are large, so
	// a coarse lat/lng grid finds one.
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	var lats, lngs []float64
	for lat := -80.0; lat <= 80.0; lat += 10.0 {
		for lng := -180.0; lng < 180.0; lng += 10.0 {
			lats = append(lats, lat)
			lngs = append(lngs, lng)
		}
	}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR1, lats, lngs, nil, nil)
	require.NoError(t, err)
	_, _, offsets, _, err := h.CellsToBoundariesE(ctx, cells, nil, nil, nil, nil)
	require.NoError(t, err)
	var distorted uint64
	var wantVertices int
	for i := range cells {
		if verts := int(offsets[i+1] - offsets[i]); verts > 6 {
			distorted = cells[i]
			wantVertices = verts
			break
		}
	}
	require.NotZero(t, distorted, "no res-1 cell with a distortion vertex found")

	outLats, outLngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, []uint64{distorted})
	require.NoError(t, err)
	requireDissolveShape(t, ctx, h, []uint64{distorted}, outLats, outLngs, ringOffsets, polygonOffsets, 1, 1, wantVertices)
}
