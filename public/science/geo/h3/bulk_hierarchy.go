//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Variable-arity return codes, in lock-step with rust/h3bridge/src/lib.rs.
const (
	growOK            uint32 = 0
	growNeedMore      uint32 = 1
	growBadResolution uint32 = 2
)

// CellsToParentsE returns the parent cell at the given coarser resolution
// for each input cell. res must be <= each cell's resolution; otherwise
// the corresponding statusDst entry is [StatusInvalidResolution].
func (inst *Handle) CellsToParentsE(
	ctx context.Context,
	res ResolutionE,
	cells []uint64,
	parentsDst []uint64,
	statusDst []StatusE,
) (parents []uint64, status []StatusE, err error) {
	n := len(cells)
	parents = slices.Grow(parentsDst[:0], n)[:n]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		return
	}

	// Scratch layout: cells(8n) | parents(8n) | status(n).
	n32 := uint32(n)
	cellsRel := uint32(0)
	parentsRel := cellsRel + n32*8
	statusRel := parentsRel + n32*8
	total := int(statusRel) + n

	var base uint32
	base, err = inst.ensureScratchE(ctx, total)
	if err != nil {
		return
	}
	cellsOff := base + cellsRel
	parentsOff := base + parentsRel
	statusOff := base + statusRel

	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}
	_, err = inst.callE(ctx, inst.fnCellToParent,
		uint64(cellsOff), uint64(n32),
		uint64(uint32(res)),
		uint64(parentsOff), uint64(statusOff),
	)
	if err != nil {
		err = eh.Errorf("h3_cell_to_parent: %w", err)
		return
	}
	err = inst.readU64sE(parentsOff, parents)
	if err != nil {
		return
	}
	err = inst.readStatusE(statusOff, status)
	return
}

// CellsToChildrenE returns the children cells at the given finer
// resolution for each input cell in CSR layout: children is the flat
// values slice, offsets has length N+1 with offsets[0]==0 and
// offsets[N]==len(children). Row i's children occupy
// children[offsets[i]:offsets[i+1]]. res must be >= each cell's
// resolution; otherwise offsets[i+1]==offsets[i] and statusDst[i] is
// [StatusInvalidResolution].
//
// Uses the one-retry grow protocol: if the provided childrenDst capacity
// is insufficient, the Rust side reports the required size and the call
// is re-issued once with a grown buffer.
func (inst *Handle) CellsToChildrenE(
	ctx context.Context,
	res ResolutionE,
	cells []uint64,
	childrenDst []uint64,
	offsetsDst []int32,
	statusDst []StatusE,
) (children []uint64, offsets []int32, status []StatusE, err error) {
	n := len(cells)
	offsets = slices.Grow(offsetsDst[:0], n+1)[:n+1]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		children = childrenDst[:0]
		offsets[0] = 0
		return
	}

	// Initial guess for the output capacity. Heuristic: two levels of
	// refinement × hexagon fan-out (49). Grown on demand via retry.
	outCap := cap(childrenDst)
	if outCap < n*49 {
		outCap = n * 49
	}

	for attempt := 0; attempt < 2; attempt++ {
		// Scratch layout: cells(8n) | offsets(4(n+1), pad to 8) |
		// children(8*outCap) | needed(4, pad to 8) | status(n).
		n32 := uint32(n)
		cellsRel := uint32(0)
		offsetsRel := cellsRel + n32*8
		childrenRel := alignUp8(offsetsRel + (n32+1)*4)
		neededRel := childrenRel + uint32(outCap)*8
		statusRel := alignUp8(neededRel + 4)
		total := int(statusRel) + n

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		cellsOff := base + cellsRel
		offsetsOff := base + offsetsRel
		childrenOff := base + childrenRel
		neededOff := base + neededRel
		statusOff := base + statusRel

		// Inputs may be lost on scratch grow; rewrite each attempt.
		err = inst.writeU64sE(cellsOff, cells)
		if err != nil {
			return
		}

		var rc uint32
		rc, err = inst.callE(ctx, inst.fnCellToChildren,
			uint64(cellsOff), uint64(n32),
			uint64(uint32(res)),
			uint64(childrenOff), uint64(offsetsOff),
			uint64(uint32(outCap)),
			uint64(neededOff), uint64(statusOff),
		)
		if err != nil {
			err = eh.Errorf("h3_cell_to_children: %w", err)
			return
		}

		switch rc {
		case growBadResolution:
			for i := range status {
				status[i] = StatusInvalidResolution
			}
			for i := range offsets {
				offsets[i] = 0
			}
			children = children[:0]
			return

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
			children = slices.Grow(childrenDst[:0], total)[:total]
			err = inst.readU64sE(childrenOff, children)
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
			err = eb.Build().Uint32("rc", rc).Errorf("h3_cell_to_children: unknown return code")
			return
		}
	}
	return
}
