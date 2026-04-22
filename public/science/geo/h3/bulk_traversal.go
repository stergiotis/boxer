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
	offsets[0] = 0
	if n == 0 {
		outCells = outCellsDst[:0]
		return
	}

	// Upper bound per cell: 3k(k+1)+1.
	perCellMax := 3*int(k)*(int(k)+1) + 1
	initialCap := n * perCellMax
	if cap(outCellsDst) > initialCap {
		initialCap = cap(outCellsDst)
	}
	outCells = slices.Grow(outCellsDst[:0], initialCap)[:initialCap]

	var cellsOff, statusOff, neededOff, offsetsOff uint32
	var outCellsOff uint32
	var outCellsOffSize int
	{ // Stage: allocate
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
		neededOff, err = inst.allocE(ctx, 4)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, neededOff, 4)
		offsetsOff, err = inst.allocE(ctx, (n+1)*4)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, offsetsOff, (n+1)*4)
	}
	defer func() {
		if outCellsOff != 0 {
			inst.freeNoE(ctx, outCellsOff, outCellsOffSize)
		}
	}()
	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}

	for attempt := 0; attempt < 2; attempt++ {
		if outCellsOff != 0 {
			inst.freeNoE(ctx, outCellsOff, outCellsOffSize)
			outCellsOff = 0
			outCellsOffSize = 0
		}
		if len(outCells) > 0 {
			outCellsOff, err = inst.allocE(ctx, len(outCells)*8)
			if err != nil {
				return
			}
			outCellsOffSize = len(outCells) * 8
		}

		var rc uint32
		{ // Stage: call
			var results []uint64
			results, err = inst.fnGridDisk.Call(
				ctx,
				uint64(cellsOff), uint64(uint32(n)),
				uint64(uint32(k)),
				uint64(outCellsOff), uint64(offsetsOff),
				uint64(uint32(len(outCells))),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				err = eh.Errorf("h3_grid_disk: %w", err)
				return
			}
			rc = uint32(results[0])
		}
		if rc == growOK {
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
			if total > len(outCells) {
				total = len(outCells)
			}
			out := make([]uint64, total)
			err = inst.readU64sE(outCellsOff, out)
			if err != nil {
				return
			}
			outCells = out
			return
		}
		if rc == growNeedMore {
			if attempt == 1 {
				err = eb.Build().Int("cap", len(outCells)).Errorf("%w", ErrGrowProtocol)
				return
			}
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			outCells = slices.Grow(outCellsDst[:0], int(needed))[:needed]
			continue
		}
		err = eb.Build().Uint32("rc", rc).Errorf("h3_grid_disk: unknown return code")
		return
	}
	return
}
