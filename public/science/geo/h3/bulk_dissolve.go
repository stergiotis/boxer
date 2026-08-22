package h3

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Dissolve return codes, in lock-step with rust/h3bridge/src/lib.rs. 0 and
// 1 are growOK / growNeedMore.
const (
	dissolveInvalidCell     uint32 = 2
	dissolveMixedResolution uint32 = 3
	dissolveDuplicate       uint32 = 4
	dissolveInternal        uint32 = 5
)

// DissolveE returns the multipolygon covered by the union of cells — the
// outline rings with their holes — as a two-level CSR: latsDeg and lngsDeg
// are parallel flat vertex slices in degrees; ringOffsets has one entry per
// ring plus one and indexes vertices; polygonOffsets has one entry per
// polygon plus one and indexes rings. Polygon p owns rings
// polygonOffsets[p] to polygonOffsets[p+1]-1, the first of which is its
// exterior and the rest its holes; ring r's vertices are
// latsDeg[ringOffsets[r]:ringOffsets[r+1]] / lngsDeg[...]. Rings are open,
// as in [Handle.CellsToBoundariesE] — append the first vertex for a closed
// ring. Vertex order is h3o's: exteriors wind counter-clockwise and holes
// clockwise, the winding the cell edges carry.
//
// The cells must share one resolution and be unique. A non-H3 cell, mixed
// resolutions or a duplicate fail the whole call with
// [ErrDissolveInvalidCell], [ErrDissolveMixedResolution] or
// [ErrDissolveDuplicateInput]; like [Handle.CompactCellsE] there is no
// per-element status, because the output has no per-input row. An empty
// input yields no polygon and offsets of length one.
//
// Uses the one-retry grow protocol on three sizes at once (vertices, rings,
// polygons); they are known only after the dissolve, so a retry dissolves
// again. The initial caps are 6*n vertices, n rings and n polygons.
func (inst *Handle) DissolveE(
	ctx context.Context,
	cells []uint64,
) (latsDeg []float64, lngsDeg []float64, ringOffsets []int32, polygonOffsets []int32, err error) {
	n := len(cells)
	if n == 0 {
		ringOffsets = []int32{0}
		polygonOffsets = []int32{0}
		return
	}

	vertexCap := n * 6
	ringCap := n
	polygonCap := n

	for attempt := range 2 {
		// Scratch layout: cells(8n) | lats(8*vertexCap) | lngs(8*vertexCap) |
		// ringOffsets(4*(ringCap+1)) | polygonOffsets(4*(polygonCap+1), pad 8) |
		// needed(3*4, pad 8).
		n32 := uint32(n)
		cellsRel := uint32(0)
		latsRel := cellsRel + n32*8
		lngsRel := latsRel + uint32(vertexCap)*8
		ringRel := lngsRel + uint32(vertexCap)*8
		polyRel := ringRel + uint32(ringCap+1)*4
		neededRel := alignUp8(polyRel + uint32(polygonCap+1)*4)
		total := int(alignUp8(neededRel + 12))

		var base uint32
		base, err = inst.ensureScratchE(ctx, total)
		if err != nil {
			return
		}
		cellsOff := base + cellsRel
		latsOff := base + latsRel
		lngsOff := base + lngsRel
		ringOff := base + ringRel
		polyOff := base + polyRel
		neededOff := base + neededRel

		err = inst.writeU64sE(cellsOff, cells)
		if err != nil {
			return
		}

		var rc uint32
		rc, err = inst.callE(ctx, inst.fnDissolve,
			uint64(cellsOff), uint64(n32),
			uint64(latsOff), uint64(lngsOff),
			uint64(ringOff), uint64(polyOff),
			uint64(uint32(vertexCap)), uint64(uint32(ringCap)), uint64(uint32(polygonCap)),
			uint64(neededOff),
		)
		if err != nil {
			err = eh.Errorf("h3_dissolve: %w", err)
			return
		}

		switch rc {
		case growOK, growNeedMore:
			var neededVertices, neededRings, neededPolygons uint32
			neededVertices, err = inst.readU32E(neededOff)
			if err != nil {
				return
			}
			neededRings, err = inst.readU32E(neededOff + 4)
			if err != nil {
				return
			}
			neededPolygons, err = inst.readU32E(neededOff + 8)
			if err != nil {
				return
			}
			if rc == growNeedMore {
				if attempt == 1 {
					err = eb.Build().Int("vertexCap", vertexCap).Int("ringCap", ringCap).Int("polygonCap", polygonCap).Errorf("%w", ErrGrowProtocol)
					return
				}
				vertexCap = int(neededVertices)
				ringCap = int(neededRings)
				polygonCap = int(neededPolygons)
				continue
			}
			nVertices := min(int(neededVertices), vertexCap)
			nRings := min(int(neededRings), ringCap)
			nPolygons := min(int(neededPolygons), polygonCap)
			latsDeg = make([]float64, nVertices)
			lngsDeg = make([]float64, nVertices)
			ringOffsets = make([]int32, nRings+1)
			polygonOffsets = make([]int32, nPolygons+1)
			err = inst.readF64sE(latsOff, latsDeg)
			if err != nil {
				return
			}
			err = inst.readF64sE(lngsOff, lngsDeg)
			if err != nil {
				return
			}
			err = inst.readI32sE(ringOff, ringOffsets)
			if err != nil {
				return
			}
			err = inst.readI32sE(polyOff, polygonOffsets)
			return

		case dissolveInvalidCell:
			err = eb.Build().Int("n", n).Errorf("%w", ErrDissolveInvalidCell)
			return

		case dissolveMixedResolution:
			err = eb.Build().Int("n", n).Errorf("%w", ErrDissolveMixedResolution)
			return

		case dissolveDuplicate:
			err = eb.Build().Int("n", n).Errorf("%w", ErrDissolveDuplicateInput)
			return

		case dissolveInternal:
			err = eb.Build().Int("n", n).Errorf("h3_dissolve: dissolution failed")
			return

		default:
			err = eb.Build().Uint32("rc", rc).Errorf("h3_dissolve: unknown return code")
			return
		}
	}
	return
}
