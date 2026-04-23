//go:build llm_generated_opus47

package h3

import (
	"context"
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LatLngsIterToCellsE is the [iter.Seq2]-input variant of
// [Handle.LatLngsToCellsE]. It accepts a stream of (index, LatLng)
// pairs where index is the row index in [0, n) — useful when the caller
// already has an Array-of-Structs source (an Arrow column, a channel,
// a parsed event stream) and materialising parallel []float64 slices
// would be redundant.
//
// The iterator must yield every index in [0, n) exactly once; gaps or
// duplicates are treated as programmer errors and surface as an error
// return. n is required up-front because the wasm scratch layout is
// sized from it; iter.Seq2 does not expose a length.
//
// This variant does *not* save work — there is one unavoidable copy
// from AoS → wasm linear memory regardless — but it removes the
// caller-side split into two []float64 slices and keeps the
// materialisation on reusable Handle-local buffers.
//
// Resolves ADR-0003 Q-W1 (SD16).
func (inst *Handle) LatLngsIterToCellsE(
	ctx context.Context,
	res ResolutionE,
	n int,
	points iter.Seq2[int, LatLng],
	cellsDst []uint64,
	statusDst []StatusE,
) (cells []uint64, status []StatusE, err error) {
	if n < 0 {
		err = eb.Build().Int("n", n).Errorf("h3: negative n")
		return
	}
	cells = slices.Grow(cellsDst[:0], n)[:n]
	status = slices.Grow(statusDst[:0], n)[:n]
	if n == 0 {
		return
	}

	// Collect the stream into handle-local Go staging buffers. A per-call
	// slices.Grow on the first run; amortised zero-alloc on subsequent
	// calls of comparable size.
	inst.iterLats = slices.Grow(inst.iterLats[:0], n)[:n]
	inst.iterLngs = slices.Grow(inst.iterLngs[:0], n)[:n]
	{ // Stage: drain iterator, validate index coverage
		seen := 0
		var covered []bool // nil until a duplicate-index is suspected
		for i, ll := range points {
			if i < 0 || i >= n {
				err = eb.Build().Int("i", i).Int("n", n).Errorf("h3: iter index out of range")
				return
			}
			if covered == nil {
				if seen == i {
					// Happy path: contiguous ascending indices; no need
					// for the coverage bitmap.
				} else {
					covered = make([]bool, n)
					for j := 0; j < seen; j++ {
						covered[j] = true
					}
				}
			}
			if covered != nil {
				if covered[i] {
					err = eb.Build().Int("i", i).Errorf("h3: iter yielded duplicate index")
					return
				}
				covered[i] = true
			}
			inst.iterLats[i] = ll.LatDeg
			inst.iterLngs[i] = ll.LngDeg
			seen++
		}
		if seen != n {
			err = eb.Build().Int("expected", n).Int("yielded", seen).Errorf("h3: iter did not cover all n indices")
			return
		}
	}

	// Scratch layout: lats(8n) | lngs(8n) | cells(8n) | status(n).
	n32 := uint32(n)
	latsRel := uint32(0)
	lngsRel := latsRel + n32*8
	cellsRel := lngsRel + n32*8
	statusRel := cellsRel + n32*8
	total := int(statusRel) + n

	var base uint32
	base, err = inst.ensureScratchE(ctx, total)
	if err != nil {
		return
	}
	latsOff := base + latsRel
	lngsOff := base + lngsRel
	cellsOff := base + cellsRel
	statusOff := base + statusRel

	err = inst.writeF64sE(latsOff, inst.iterLats)
	if err != nil {
		return
	}
	err = inst.writeF64sE(lngsOff, inst.iterLngs)
	if err != nil {
		return
	}
	_, err = inst.callE(ctx, inst.fnLatLngToCell,
		uint64(latsOff), uint64(lngsOff),
		uint64(n32),
		uint64(uint32(res)),
		uint64(cellsOff), uint64(statusOff),
	)
	if err != nil {
		err = eh.Errorf("h3_latlng_to_cell: %w", err)
		return
	}
	err = inst.readU64sE(cellsOff, cells)
	if err != nil {
		return
	}
	err = inst.readStatusE(statusOff, status)
	return
}
