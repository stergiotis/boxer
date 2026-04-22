//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// GridDisksE returns the k-ring neighbourhood of each input cell in CSR
// layout: outCells holds all neighbours concatenated, offsets has length
// N+1 with offsets[0]==0 and offsets[i+1]-offsets[i] == ringSize(cells[i]).
// Row i's neighbours occupy outCells[offsets[i]:offsets[i+1]].
//
// k==0 returns each cell itself.
//
// Uses the one-retry grow protocol.
func (inst *Handle) GridDisksE(
	ctx context.Context,
	k uint8,
	cells []uint64,
	outCellsDst []uint64,
	offsetsDst []int32,
	statusDst []StatusE,
) (outCells []uint64, offsets []int32, status []StatusE, err error) {
	n := len(cells)
	offsets = slices.Grow(offsetsDst[:0], n+1)[:n+1]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		outCells = outCellsDst[:0]
		offsets[0] = 0
		return
	}

	// Per-cell upper bound (non-pentagon): 3k(k+1)+1.
	perCellMax := 3*int(k)*(int(k)+1) + 1
	outCap := n * perCellMax
	if cap(outCellsDst) > outCap {
		outCap = cap(outCellsDst)
	}

	for attempt := 0; attempt < 2; attempt++ {
		n32 := uint32(n)
		cellsRel := uint32(0)
		offsetsRel := cellsRel + n32*8
		outRel := alignUp8(offsetsRel + (n32+1)*4)
		neededRel := outRel + uint32(outCap)*8
		statusRel := alignUp8(neededRel + 4)
		total := int(statusRel) + n

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		cellsOff := base + cellsRel
		offsetsOff := base + offsetsRel
		outOff := base + outRel
		neededOff := base + neededRel
		statusOff := base + statusRel

		err = inst.writeU64sE(cellsOff, cells)
		if err != nil {
			return
		}

		var rc uint32
		{ // Stage: call
			var results []uint64
			results, err = inst.fnGridDisk.Call(
				ctx,
				uint64(cellsOff), uint64(n32),
				uint64(uint32(k)),
				uint64(outOff), uint64(offsetsOff),
				uint64(uint32(outCap)),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				err = eh.Errorf("h3_grid_disk: %w", err)
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
			if total > outCap {
				total = outCap
			}
			outCells = slices.Grow(outCellsDst[:0], total)[:total]
			err = inst.readU64sE(outOff, outCells)
			return

		case growNeedMore:
			if attempt == 1 {
				err = eb.Build().Int("cap", outCap).Errorf("%w", ErrGrowProtocol)
				return
			}
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			outCap = int(needed)

		default:
			err = eb.Build().Uint32("rc", rc).Errorf("h3_grid_disk: unknown return code")
			return
		}
	}
	return
}
