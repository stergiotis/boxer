//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LatLngsToCellsE converts parallel latitude/longitude slices (degrees)
// into H3 cell indices at the given resolution. Per-element validity is
// reported in statusDst. Bulk-level error is reserved for WASM traps and
// memory-bound violations.
//
// latsDeg and lngsDeg must have the same length. cellsDst and statusDst
// are grown via [slices.Grow] and returned.
func (inst *Handle) LatLngsToCellsE(
	ctx context.Context,
	res ResolutionE,
	latsDeg []float64,
	lngsDeg []float64,
	cellsDst []uint64,
	statusDst []StatusE,
) (cells []uint64, status []StatusE, err error) {
	if len(latsDeg) != len(lngsDeg) {
		err = eb.Build().Int("lats", len(latsDeg)).Int("lngs", len(lngsDeg)).Errorf("h3: lat/lng length mismatch")
		return
	}
	n := len(latsDeg)
	cells = slices.Grow(cellsDst[:0], n)[:n]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		return
	}

	var latsOff, lngsOff, cellsOff, statusOff uint32
	{ // Stage: allocate wasm regions
		latsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, latsOff, n*8)
		lngsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, lngsOff, n*8)
		cellsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, cellsOff, n*8)
		statusOff, err = inst.allocE(ctx, n)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, statusOff, n)
	}
	{ // Stage: stage inputs
		err = inst.writeF64sE(latsOff, latsDeg)
		if err != nil {
			return
		}
		err = inst.writeF64sE(lngsOff, lngsDeg)
		if err != nil {
			return
		}
	}
	{ // Stage: call
		_, err = inst.fnLatLngToCell.Call(
			ctx,
			uint64(latsOff), uint64(lngsOff),
			uint64(uint32(n)),
			uint64(uint32(res)),
			uint64(cellsOff), uint64(statusOff),
		)
		if err != nil {
			err = eh.Errorf("h3_latlng_to_cell: %w", err)
			return
		}
	}
	{ // Stage: read outputs
		err = inst.readU64sE(cellsOff, cells)
		if err != nil {
			return
		}
		err = inst.readStatusE(statusOff, status)
		if err != nil {
			return
		}
	}
	return
}

// CellsToLatLngsE returns the lat/lng (in degrees) of each H3 cell's center
// point. latsDegDst and lngsDegDst are grown via [slices.Grow] and returned.
func (inst *Handle) CellsToLatLngsE(
	ctx context.Context,
	cells []uint64,
	latsDegDst []float64,
	lngsDegDst []float64,
	statusDst []StatusE,
) (latsDeg []float64, lngsDeg []float64, status []StatusE, err error) {
	n := len(cells)
	latsDeg = slices.Grow(latsDegDst[:0], n)[:n]
	lngsDeg = slices.Grow(lngsDegDst[:0], n)[:n]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		return
	}

	var cellsOff, latsOff, lngsOff, statusOff uint32
	{ // Stage: allocate
		cellsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, cellsOff, n*8)
		latsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, latsOff, n*8)
		lngsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, lngsOff, n*8)
		statusOff, err = inst.allocE(ctx, n)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, statusOff, n)
	}
	{ // Stage: stage inputs
		err = inst.writeU64sE(cellsOff, cells)
		if err != nil {
			return
		}
	}
	{ // Stage: call
		_, err = inst.fnCellToLatLng.Call(
			ctx,
			uint64(cellsOff), uint64(uint32(n)),
			uint64(latsOff), uint64(lngsOff), uint64(statusOff),
		)
		if err != nil {
			err = eh.Errorf("h3_cell_to_latlng: %w", err)
			return
		}
	}
	{ // Stage: read outputs
		err = inst.readF64sE(latsOff, latsDeg)
		if err != nil {
			return
		}
		err = inst.readF64sE(lngsOff, lngsDeg)
		if err != nil {
			return
		}
		err = inst.readStatusE(statusOff, status)
		if err != nil {
			return
		}
	}
	return
}
