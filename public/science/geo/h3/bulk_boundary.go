//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CellsToBoundariesE returns the polygonal boundary of each H3 cell in CSR
// layout: latsDeg and lngsDeg are parallel flat []float64 slices of
// vertices, offsets has length N+1 with offsets[0]==0 and
// offsets[N]==len(latsDeg). Row i's open ring is
// latsDeg[offsets[i]:offsets[i+1]] / lngsDeg[offsets[i]:offsets[i+1]];
// append latsDeg[offsets[i]] and lngsDeg[offsets[i]] if a closed ring is
// needed. Invalid input cells flag [StatusInvalidCell] and produce a
// zero-length row.
//
// Typical vertex count is 6 (hexagon) or 5 (pentagon); pentagons whose
// boundary crosses an icosahedron face edge can reach up to 10. Uses the
// one-retry grow protocol; the initial heuristic cap is 6*n vertices.
func (inst *Handle) CellsToBoundariesE(
	ctx context.Context,
	cells []uint64,
	latsDegDst []float64,
	lngsDegDst []float64,
	offsetsDst []int32,
	statusDst []StatusE,
) (latsDeg []float64, lngsDeg []float64, offsets []int32, status []StatusE, err error) {
	n := len(cells)
	offsets = slices.Grow(offsetsDst[:0], n+1)[:n+1]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		latsDeg = latsDegDst[:0]
		lngsDeg = lngsDegDst[:0]
		offsets[0] = 0
		return
	}

	vertexCap := cap(latsDegDst)
	if vertexCap < cap(lngsDegDst) {
		vertexCap = cap(lngsDegDst)
	}
	if vertexCap < n*6 {
		vertexCap = n * 6
	}

	for attempt := 0; attempt < 2; attempt++ {
		n32 := uint32(n)
		cellsRel := uint32(0)
		latsRel := cellsRel + n32*8
		lngsRel := latsRel + uint32(vertexCap)*8
		offsetsRel := lngsRel + uint32(vertexCap)*8
		neededRel := alignUp8(offsetsRel + (n32+1)*4)
		statusRel := alignUp8(neededRel + 4)
		total := int(statusRel) + n

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		cellsOff := base + cellsRel
		latsOff := base + latsRel
		lngsOff := base + lngsRel
		offsetsOff := base + offsetsRel
		neededOff := base + neededRel
		statusOff := base + statusRel

		err = inst.writeU64sE(cellsOff, cells)
		if err != nil {
			return
		}

		var rc uint32
		{ // Stage: call
			var results []uint64
			results, err = inst.fnCellToBoundary.Call(
				ctx,
				uint64(cellsOff), uint64(n32),
				uint64(latsOff), uint64(lngsOff), uint64(offsetsOff),
				uint64(uint32(vertexCap)),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				err = eh.Errorf("h3_cell_to_boundary: %w", err)
				return
			}
			rc = uint32(results[0])
		}

		switch rc {
		case growOK:
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			err = inst.readI32sE(offsetsOff, offsets)
			if err != nil {
				return
			}
			err = inst.readStatusE(statusOff, status)
			if err != nil {
				return
			}
			total := int(needed)
			if total > vertexCap {
				total = vertexCap
			}
			latsDeg = slices.Grow(latsDegDst[:0], total)[:total]
			lngsDeg = slices.Grow(lngsDegDst[:0], total)[:total]
			err = inst.readF64sE(latsOff, latsDeg)
			if err != nil {
				return
			}
			err = inst.readF64sE(lngsOff, lngsDeg)
			return

		case growNeedMore:
			if attempt == 1 {
				err = eb.Build().Int("cap", vertexCap).Errorf("%w", ErrGrowProtocol)
				return
			}
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			vertexCap = int(needed)

		default:
			err = eb.Build().Uint32("rc", rc).Errorf("h3_cell_to_boundary: unknown return code")
			return
		}
	}
	return
}
