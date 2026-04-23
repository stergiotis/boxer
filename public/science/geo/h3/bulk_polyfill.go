//go:build llm_generated_opus47

package h3

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Polyfill-specific return codes, in lock-step with rust/h3bridge/src/lib.rs.
const (
	polyfillBadMode     uint32 = 2
	polyfillBadGeometry uint32 = 3
)

// PolygonToCellsE returns the H3 cells at res that cover the given polygon
// under the requested containment mode. vertsLat and vertsLng are parallel
// []float64 slices of all vertices across all rings; ringOffsets has
// length ringCount+1, with ringOffsets[0] == 0, ringOffsets[ringCount] ==
// len(vertsLat), and monotone non-decreasing. The first ring is the
// exterior boundary; subsequent rings (if any) are holes.
//
// Single-polygon API: callers with multiple polygons loop. Variable-arity
// output uses the one-retry grow protocol.
func (inst *Handle) PolygonToCellsE(
	ctx context.Context,
	res ResolutionE,
	mode ContainmentModeE,
	vertsLat []float64,
	vertsLng []float64,
	ringOffsets []int32,
	cellsDst []uint64,
) (cells []uint64, err error) {
	if len(vertsLat) != len(vertsLng) {
		err = eb.Build().Int("lats", len(vertsLat)).Int("lngs", len(vertsLng)).Errorf("h3: vert lat/lng length mismatch")
		return
	}
	if len(ringOffsets) < 2 {
		err = eb.Build().Int("ringOffsets", len(ringOffsets)).Errorf("h3: PolygonToCellsE needs at least one ring (ringOffsets length >= 2)")
		return
	}
	ringCount := len(ringOffsets) - 1
	totalVerts := len(vertsLat)
	if int(ringOffsets[0]) != 0 || int(ringOffsets[ringCount]) != totalVerts {
		err = eb.Build().Int("total", totalVerts).Int32("first", ringOffsets[0]).Int32("last", ringOffsets[ringCount]).Errorf("h3: ring offsets do not bound vert slice")
		return
	}

	// Heuristic initial output cap: one cell per 4 exterior vertices, at
	// least 64. Grown via one-retry on need-more.
	outCap := cap(cellsDst)
	if outCap < 64 {
		outCap = 64
	}
	if outCap < totalVerts*8 {
		outCap = totalVerts * 8
	}

	for attempt := 0; attempt < 2; attempt++ {
		// Scratch layout: lats(8n) | lngs(8n) | ringOffsets(4(rc+1), pad 8) |
		// cells(8*outCap) | needed(4, pad 8).
		nV := uint32(totalVerts)
		rC := uint32(ringCount)
		latsRel := uint32(0)
		lngsRel := latsRel + nV*8
		ringRel := lngsRel + nV*8
		cellsRel := alignUp8(ringRel + (rC+1)*4)
		neededRel := cellsRel + uint32(outCap)*8
		total := int(alignUp8(neededRel + 4))

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		latsOff := base + latsRel
		lngsOff := base + lngsRel
		ringOff := base + ringRel
		cellsOff := base + cellsRel
		neededOff := base + neededRel

		err = inst.writeF64sE(latsOff, vertsLat)
		if err != nil {
			return
		}
		err = inst.writeF64sE(lngsOff, vertsLng)
		if err != nil {
			return
		}
		err = inst.writeI32sE(ringOff, ringOffsets)
		if err != nil {
			return
		}

		var rc uint32
		rc, err = inst.callE(ctx, inst.fnPolygonToCells,
			uint64(latsOff), uint64(lngsOff),
			uint64(ringOff), uint64(rC),
			uint64(uint32(res)), uint64(uint32(mode)),
			uint64(cellsOff),
			uint64(uint32(outCap)),
			uint64(neededOff),
		)
		if err != nil {
			err = eh.Errorf("h3_polygon_to_cells: %w", err)
			return
		}

		switch rc {
		case growOK:
			var needed uint32
			needed, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			total := int(needed)
			if total > outCap {
				total = outCap
			}
			cells = slices.Grow(cellsDst[:0], total)[:total]
			err = inst.readU64sE(cellsOff, cells)
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

		case polyfillBadMode:
			err = eb.Build().Uint8("res", uint8(res)).Uint8("mode", uint8(mode)).Errorf("%w", ErrBadContainmentMode)
			return

		case polyfillBadGeometry:
			err = eb.Build().Int("rings", ringCount).Int("verts", totalVerts).Errorf("%w", ErrBadPolygonGeometry)
			return

		default:
			err = eb.Build().Uint32("rc", rc).Errorf("h3_polygon_to_cells: unknown return code")
			return
		}
	}
	return
}
