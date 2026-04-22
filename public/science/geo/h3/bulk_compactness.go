//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Compact return codes in lock-step with rust/h3bridge/src/lib.rs.
const (
	compactOK               uint32 = 0
	compactMixedResolution  uint32 = 1
	compactDuplicate        uint32 = 2
	compactInvalidCellInput uint32 = 3
)

// CompactCellsE collapses a set of H3 cells at a single resolution into the
// smallest equivalent set possible at mixed (coarser) resolutions. Input
// cells must all be at the same resolution and must be unique; both
// conditions surface as [ErrCompactMixedResolution] or
// [ErrCompactDuplicateInput] respectively.
//
// No per-element status slice: compact has no stable 1:1 mapping from input
// to output (by design). See ADR-0003 Updates SD13 for the rationale.
func (inst *Handle) CompactCellsE(
	ctx context.Context,
	cells []uint64,
	compactedDst []uint64,
) (compacted []uint64, err error) {
	n := len(cells)
	if n == 0 {
		compacted = compactedDst[:0]
		return
	}

	// Scratch layout: cells(8n) | out(8n) | count(4).
	n32 := uint32(n)
	cellsRel := uint32(0)
	outRel := cellsRel + n32*8
	countRel := outRel + n32*8
	total := int(alignUp8(countRel + 4))

	var base uint32
	base, err = inst.ensureScratchE(ctx, total)
	if err != nil {
		return
	}
	cellsOff := base + cellsRel
	outOff := base + outRel
	countOff := base + countRel

	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}

	var rc uint32
	{ // Stage: call
		var results []uint64
		results, err = inst.fnCompactCells.Call(
			ctx,
			uint64(cellsOff), uint64(n32),
			uint64(outOff), uint64(countOff),
		)
		if err != nil {
			err = eh.Errorf("h3_compact_cells: %w", err)
			return
		}
		rc = uint32(results[0])
	}

	switch rc {
	case compactOK:
		var count uint32
		count, err = inst.readU32E(countOff)
		if err != nil {
			return
		}
		if int(count) > n {
			count = uint32(n)
		}
		compacted = slices.Grow(compactedDst[:0], int(count))[:count]
		err = inst.readU64sE(outOff, compacted)
		return
	case compactMixedResolution:
		err = eb.Build().Int("n", n).Errorf("%w", ErrCompactMixedResolution)
		return
	case compactDuplicate:
		err = eb.Build().Int("n", n).Errorf("%w", ErrCompactDuplicateInput)
		return
	case compactInvalidCellInput:
		err = eb.Build().Int("n", n).Errorf("h3: compact input contains a non-H3 cell")
		return
	default:
		err = eb.Build().Uint32("rc", rc).Errorf("h3_compact_cells: unknown return code")
		return
	}
}

// UncompactCellsE expands a (possibly mixed-resolution) set of cells into
// a flat set of cells at the target resolution res. Each input cell whose
// resolution is finer than res is skipped and flagged
// [StatusInvalidResolution] in statusDst; invalid cell inputs are flagged
// [StatusInvalidCell]. The output is flat — not CSR — so consumers lose
// per-input provenance; use [Handle.CellsToChildrenE] when provenance
// matters (see ADR-0003 Updates SD14).
//
// Uses the one-retry grow protocol identical to [Handle.CellsToChildrenE].
func (inst *Handle) UncompactCellsE(
	ctx context.Context,
	res ResolutionE,
	cells []uint64,
	expandedDst []uint64,
	statusDst []StatusE,
) (expanded []uint64, status []StatusE, err error) {
	n := len(cells)
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		expanded = expandedDst[:0]
		return
	}

	outCap := cap(expandedDst)
	if outCap < n*49 {
		outCap = n * 49
	}

	for attempt := 0; attempt < 2; attempt++ {
		n32 := uint32(n)
		cellsRel := uint32(0)
		outRel := cellsRel + n32*8
		neededRel := outRel + uint32(outCap)*8
		statusRel := alignUp8(neededRel + 4)
		total := int(statusRel) + n

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		cellsOff := base + cellsRel
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
			results, err = inst.fnUncompactCells.Call(
				ctx,
				uint64(cellsOff), uint64(n32),
				uint64(uint32(res)),
				uint64(outOff),
				uint64(uint32(outCap)),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				err = eh.Errorf("h3_uncompact_cells: %w", err)
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
			err = inst.readStatusE(statusOff, status)
			if err != nil {
				return
			}
			total := int(needed)
			if total > outCap {
				total = outCap
			}
			expanded = slices.Grow(expandedDst[:0], total)[:total]
			err = inst.readU64sE(outOff, expanded)
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

		case growBadResolution:
			err = eb.Build().Uint8("res", uint8(res)).Errorf("h3_uncompact_cells: bad resolution")
			return

		default:
			err = eb.Build().Uint32("rc", rc).Errorf("h3_uncompact_cells: unknown return code")
			return
		}
	}
	return
}
