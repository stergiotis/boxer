//go:build llm_generated_opus47

package h3

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Scalar convenience wrappers. These are thin shims over the bulk API for
// the common "one input / one output" case — primarily UI glue and ad-hoc
// scripting where the ceremony of building 1-element slices, passing
// `nil` dst slices, and indexing `[0]` on return obscures the intent.
//
// Every wrapper here is implemented by calling the matching `*sE` / `*sE`-
// style bulk method. Hot paths with N > ~8 elements should still call the
// bulk form directly to amortise per-call overhead across the batch.

// LatLngToCellE converts a single lat/lng (degrees) to an H3 cell at the
// given resolution. Returns the per-element status as its own value so
// callers can triage without allocating a status slice.
//
// Bulk equivalent: [Handle.LatLngsToCellsE].
func (inst *Handle) LatLngToCellE(
	ctx context.Context,
	res ResolutionE,
	latDeg float64,
	lngDeg float64,
) (cell uint64, status StatusE, err error) {
	var cells []uint64
	var statuses []StatusE
	cells, statuses, err = inst.LatLngsToCellsE(ctx,
		res, []float64{latDeg}, []float64{lngDeg}, nil, nil)
	if err != nil {
		err = eh.Errorf("h3: LatLngToCellE: %w", err)
		return
	}
	if len(cells) != 1 || len(statuses) != 1 {
		err = eh.Errorf("h3: LatLngToCellE: bulk API returned %d cells / %d statuses", len(cells), len(statuses))
		return
	}
	cell = cells[0]
	status = statuses[0]
	return
}

// CellToLatLngE returns a single cell's centroid lat/lng (degrees). Status
// reports per-element validity: non-Ok means the cell index did not parse.
//
// Bulk equivalent: [Handle.CellsToLatLngsE].
func (inst *Handle) CellToLatLngE(
	ctx context.Context,
	cell uint64,
) (latDeg float64, lngDeg float64, status StatusE, err error) {
	var lats, lngs []float64
	var statuses []StatusE
	lats, lngs, statuses, err = inst.CellsToLatLngsE(ctx,
		[]uint64{cell}, nil, nil, nil)
	if err != nil {
		err = eh.Errorf("h3: CellToLatLngE: %w", err)
		return
	}
	if len(lats) != 1 || len(lngs) != 1 || len(statuses) != 1 {
		err = eh.Errorf("h3: CellToLatLngE: bulk API returned %d lats / %d lngs / %d statuses", len(lats), len(lngs), len(statuses))
		return
	}
	latDeg = lats[0]
	lngDeg = lngs[0]
	status = statuses[0]
	return
}

// GridDiskE returns the k-ring neighbourhood of a single cell as a flat
// []uint64 — no CSR offsets, since there is only one input row. k==0
// returns a single-element slice containing the input cell itself.
//
// Bulk equivalent: [Handle.GridDisksE].
func (inst *Handle) GridDiskE(
	ctx context.Context,
	k uint8,
	cell uint64,
) (neighbours []uint64, status StatusE, err error) {
	var out []uint64
	var offsets []int32
	var statuses []StatusE
	out, offsets, statuses, err = inst.GridDisksE(ctx,
		k, []uint64{cell}, nil, nil, nil)
	if err != nil {
		err = eh.Errorf("h3: GridDiskE: %w", err)
		return
	}
	if len(offsets) != 2 || len(statuses) != 1 {
		err = eh.Errorf("h3: GridDiskE: bulk API returned %d offsets / %d statuses", len(offsets), len(statuses))
		return
	}
	// Slice is valid when statuses[0] == StatusOk. On non-Ok, the row
	// may still be present (empty or otherwise). Callers should branch
	// on status.
	neighbours = out[offsets[0]:offsets[1]]
	status = statuses[0]
	return
}

// PolygonToCellsSimpleE covers the common "one exterior ring, no holes"
// polygon-to-cells shape without the ring-offsets ceremony. vertsLat and
// vertsLng describe one closed ring; the wrapper synthesises ringOffsets
// automatically.
//
// For multi-ring polygons (holes), use [Handle.PolygonToCellsE] directly.
//
// Bulk equivalent: [Handle.PolygonToCellsE] with
// `ringOffsets = []int32{0, int32(len(vertsLat))}`.
func (inst *Handle) PolygonToCellsSimpleE(
	ctx context.Context,
	res ResolutionE,
	mode ContainmentModeE,
	vertsLat []float64,
	vertsLng []float64,
) (cells []uint64, err error) {
	cells, err = inst.PolygonToCellsE(ctx,
		res, mode, vertsLat, vertsLng,
		[]int32{0, int32(len(vertsLat))}, nil)
	if err != nil {
		err = eh.Errorf("h3: PolygonToCellsSimpleE: %w", err)
		return
	}
	return
}
