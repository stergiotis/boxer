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
	offsets[0] = 0
	if n == 0 {
		buf = bufDst[:0]
		return
	}

	// H3 strings are ≤ 16 bytes each.
	initialCap := n * 16
	if cap(bufDst) > initialCap {
		initialCap = cap(bufDst)
	}
	buf = slices.Grow(bufDst[:0], initialCap)[:initialCap]

	var cellsOff, statusOff, neededOff, offsetsOff uint32
	var bufOff uint32
	var bufOffSize int
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
		if bufOff != 0 {
			inst.freeNoE(ctx, bufOff, bufOffSize)
		}
	}()
	err = inst.writeU64sE(cellsOff, cells)
	if err != nil {
		return
	}

	for attempt := 0; attempt < 2; attempt++ {
		if bufOff != 0 {
			inst.freeNoE(ctx, bufOff, bufOffSize)
			bufOff = 0
			bufOffSize = 0
		}
		if len(buf) > 0 {
			bufOff, err = inst.allocE(ctx, len(buf))
			if err != nil {
				return
			}
			bufOffSize = len(buf)
		}

		var rc uint32
		{ // Stage: call
			var results []uint64
			results, err = inst.fnCellToString.Call(
				ctx,
				uint64(cellsOff), uint64(uint32(n)),
				uint64(bufOff), uint64(offsetsOff),
				uint64(uint32(len(buf))),
				uint64(neededOff), uint64(statusOff),
			)
			if err != nil {
				err = eh.Errorf("h3_cell_to_string: %w", err)
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
			if total > len(buf) {
				total = len(buf)
			}
			var raw []byte
			raw, err = inst.readBytesE(bufOff, total)
			if err != nil {
				return
			}
			out := make([]byte, total)
			copy(out, raw)
			buf = out
			return
		}
		if rc == growNeedMore {
			if attempt == 1 {
				err = eb.Build().Int("cap", len(buf)).Errorf("%w", ErrGrowProtocol)
				return
			}
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			buf = slices.Grow(bufDst[:0], int(needed))[:needed]
			continue
		}
		err = eb.Build().Uint32("rc", rc).Errorf("h3_cell_to_string: unknown return code")
		return
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

	var bufOff, offsetsOff, cellsOff, statusOff uint32
	{ // Stage: allocate
		if len(buf) > 0 {
			bufOff, err = inst.allocE(ctx, len(buf))
			if err != nil {
				return
			}
			defer inst.freeNoE(ctx, bufOff, len(buf))
		}
		offsetsOff, err = inst.allocE(ctx, (n+1)*4)
		if err != nil {
			return
		}
		defer inst.freeNoE(ctx, offsetsOff, (n+1)*4)
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
		err = inst.writeBytesE(bufOff, buf)
		if err != nil {
			return
		}
		err = inst.writeI32sE(offsetsOff, offsets)
		if err != nil {
			return
		}
	}
	{ // Stage: call
		_, err = inst.fnStringToCell.Call(
			ctx,
			uint64(bufOff), uint64(offsetsOff),
			uint64(uint32(n)),
			uint64(cellsOff), uint64(statusOff),
		)
		if err != nil {
			err = eh.Errorf("h3_string_to_cell: %w", err)
			return
		}
	}
	{ // Stage: read outputs
		err = inst.readU64sE(cellsOff, cells)
		if err != nil {
			return
		}
		err = inst.readStatusE(statusOff, status)
	}
	return
}
