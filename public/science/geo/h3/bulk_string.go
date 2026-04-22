//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CellsToStringsE encodes cell indices as their H3 hex-string form in CSR
// layout: buf holds all strings concatenated (no separators, no NUL),
// offsets has length N+1 with offsets[0]==0, string i occupying
// buf[offsets[i]:offsets[i+1]].
//
// Uses the one-retry grow protocol.
func (inst *Handle) CellsToStringsE(
	ctx context.Context,
	cells []uint64,
	bufDst []byte,
	offsetsDst []int32,
	statusDst []StatusE,
) (buf []byte, offsets []int32, status []StatusE, err error) {
	n := len(cells)
	offsets = slices.Grow(offsetsDst[:0], n+1)[:n+1]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		buf = bufDst[:0]
		offsets[0] = 0
		return
	}

	// H3 strings are <= 16 bytes each.
	outCap := n * 16
	if cap(bufDst) > outCap {
		outCap = cap(bufDst)
	}

	for attempt := 0; attempt < 2; attempt++ {
		n32 := uint32(n)
		cellsRel := uint32(0)
		offsetsRel := cellsRel + n32*8
		bufRel := offsetsRel + (n32+1)*4
		neededRel := alignUp8(bufRel + uint32(outCap))
		statusRel := alignUp8(neededRel + 4)
		total := int(statusRel) + n

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		cellsOff := base + cellsRel
		offsetsOff := base + offsetsRel
		bufOff := base + bufRel
		neededOff := base + neededRel
		statusOff := base + statusRel

		err = inst.writeU64sE(cellsOff, cells)
		if err != nil {
			return
		}

		var rc uint32
		{ // Stage: call
			var results []uint64
			results, err = inst.fnCellToString.Call(
				ctx,
				uint64(cellsOff), uint64(n32),
				uint64(bufOff), uint64(offsetsOff),
				uint64(uint32(outCap)),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				err = eh.Errorf("h3_cell_to_string: %w", err)
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
			var raw []byte
			raw, err = inst.readBytesE(bufOff, total)
			if err != nil {
				return
			}
			// raw aliases guest memory; copy to a caller-owned slice.
			buf = slices.Grow(bufDst[:0], total)[:total]
			copy(buf, raw)
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
			err = eb.Build().Uint32("rc", rc).Errorf("h3_cell_to_string: unknown return code")
			return
		}
	}
	return
}

// StringsToCellsE decodes cell indices from their H3 hex-string form in
// CSR layout. buf and offsets describe N strings exactly as
// [Handle.CellsToStringsE] would emit them; offsets must have length N+1
// where N is the inferred batch size (offsets[N] == len(buf)).
func (inst *Handle) StringsToCellsE(
	ctx context.Context,
	buf []byte,
	offsets []int32,
	cellsDst []uint64,
	statusDst []StatusE,
) (cells []uint64, status []StatusE, err error) {
	if len(offsets) == 0 {
		err = eb.Build().Errorf("h3: StringsToCellsE requires offsets of length N+1 (got 0)")
		return
	}
	n := len(offsets) - 1
	cells = slices.Grow(cellsDst[:0], n)[:n]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		return
	}
	if offsets[0] != 0 {
		err = eb.Build().Int32("first", offsets[0]).Errorf("h3: StringsToCellsE: offsets[0] must be 0")
		return
	}
	if int(offsets[n]) != len(buf) {
		err = eb.Build().Int("bufLen", len(buf)).Int32("offLast", offsets[n]).Errorf("h3: StringsToCellsE: offsets[N] != len(buf)")
		return
	}

	// Scratch layout: buf(len(buf), pad to 4) | offsets(4(n+1), pad to 8) | cells(8n) | status(n).
	n32 := uint32(n)
	bufLen := uint32(len(buf))
	bufRel := uint32(0)
	offsetsRel := alignUp8(bufRel + bufLen) // i32 needs 4-byte; align to 8 for the u64 that follows
	if offsetsRel < bufRel+bufLen {
		offsetsRel = bufRel + bufLen
	}
	cellsRel := alignUp8(offsetsRel + (n32+1)*4)
	statusRel := cellsRel + n32*8
	total := int(statusRel) + n

	var base uint32
	base, err = inst.ensureScratchE(ctx, total)
	if err != nil {
		return
	}
	bufOff := base + bufRel
	offsetsOff := base + offsetsRel
	cellsOff := base + cellsRel
	statusOff := base + statusRel

	err = inst.writeBytesE(bufOff, buf)
	if err != nil {
		return
	}
	err = inst.writeI32sE(offsetsOff, offsets)
	if err != nil {
		return
	}
	_, err = inst.fnStringToCell.Call(
		ctx,
		uint64(bufOff), uint64(offsetsOff),
		uint64(n32),
		uint64(cellsOff), uint64(statusOff),
	)
	if err != nil {
		err = eh.Errorf("h3_string_to_cell: %w", err)
		return
	}
	err = inst.readU64sE(cellsOff, cells)
	if err != nil {
		return
	}
	err = inst.readStatusE(statusOff, status)
	return
}
