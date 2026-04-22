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
	var cellsOff, parentsOff, statusOff uint32
	{ // Stage: allocate
		cellsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, cellsOff, n*8)
		parentsOff, err = inst.allocE(ctx, n*8)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, parentsOff, n*8)
		statusOff, err = inst.allocE(ctx, n)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, statusOff, n)
	}
	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}
	_, err = inst.fnCellToParent.Call(
		ctx,
		uint64(cellsOff), uint64(uint32(n)),
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
	offsets[0] = 0
	if n == 0 {
		children = childrenDst[:0]
		return
	}

	childrenCap := cap(childrenDst)
	if childrenCap < n {
		childrenCap = n
	}
	children = slices.Grow(childrenDst[:0], childrenCap)[:childrenCap]

	var cellsOff, statusOff, neededOff, offsetsOff, childrenOff uint32
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
	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}

	// One-retry grow loop.
	for attempt := 0; attempt < 2; attempt++ {
		if childrenOff != 0 {
			inst.freeNoE(ctx, childrenOff, len(children)*8)
			childrenOff = 0
		}
		if len(children) > 0 {
			childrenOff, err = inst.allocE(ctx, len(children)*8)
			if err != nil {
				return
			}
		}

		var rc uint32
		{ // Stage: call
			var results []uint64
			results, err = inst.fnCellToChildren.Call(
				ctx,
				uint64(cellsOff), uint64(uint32(n)),
				uint64(uint32(res)),
				uint64(childrenOff), uint64(offsetsOff),
				uint64(uint32(len(children))),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				if childrenOff != 0 {
					inst.freeNoE(ctx, childrenOff, len(children)*8)
				}
				err = eh.Errorf("h3_cell_to_children: %w", err)
				return
			}
			rc = uint32(results[0])
		}
		if rc == growBadResolution {
			for i := range status {
				status[i] = StatusInvalidResolution
			}
			for i := range offsets {
				offsets[i] = 0
			}
			children = children[:0]
			if childrenOff != 0 {
				inst.freeNoE(ctx, childrenOff, len(children)*8)
			}
			return
		}
		if rc == growOK {
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				if childrenOff != 0 {
					inst.freeNoE(ctx, childrenOff, len(children)*8)
				}
				return
			}
			err = inst.readI32sE(offsetsOff, offsets)
			if err != nil {
				if childrenOff != 0 {
					inst.freeNoE(ctx, childrenOff, len(children)*8)
				}
				return
			}
			err = inst.readStatusE(statusOff, status)
			if err != nil {
				if childrenOff != 0 {
					inst.freeNoE(ctx, childrenOff, len(children)*8)
				}
				return
			}
			total := int(needed)
			if total > len(children) {
				total = len(children)
			}
			out := make([]uint64, total)
			err = inst.readU64sE(childrenOff, out)
			if childrenOff != 0 {
				inst.freeNoE(ctx, childrenOff, len(children)*8)
			}
			if err != nil {
				return
			}
			children = out
			return
		}
		if rc == growNeedMore {
			if attempt == 1 {
				err = eb.Build().Int("cap", len(children)).Errorf("%w", ErrGrowProtocol)
				if childrenOff != 0 {
					inst.freeNoE(ctx, childrenOff, len(children)*8)
				}
				return
			}
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				if childrenOff != 0 {
					inst.freeNoE(ctx, childrenOff, len(children)*8)
				}
				return
			}
			if childrenOff != 0 {
				inst.freeNoE(ctx, childrenOff, len(children)*8)
				childrenOff = 0
			}
			children = slices.Grow(childrenDst[:0], int(needed))[:needed]
			continue
		}
		err = eb.Build().Uint32("rc", rc).Errorf("h3_cell_to_children: unknown return code")
		if childrenOff != 0 {
			inst.freeNoE(ctx, childrenOff, len(children)*8)
		}
		return
	}
	return
}
