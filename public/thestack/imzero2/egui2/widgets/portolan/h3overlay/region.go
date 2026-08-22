package h3overlay

import (
	"context"
	"hash/maphash"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/geo/h3"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// Region is a set of cells drawn as one area: every cell filled, the outline
// of their union stroked (the dissolve, h3's `h3_dissolve` through the
// bridge), and a label at the centroid of the largest outline ring. The
// dissolve is cached by the cell set, so a region whose cells do not change
// pays it once; the fills and the projection are per frame. The zero value
// is ready to use.
type Region struct {
	cells Layer

	key       uint64
	n         int
	ringLats  [][]float64 // each ring closed (first vertex repeated)
	ringLngs  [][]float64
	labelAt   portolan.LatLng
	haveLabel bool
}

var regionSeed = maphash.MakeSeed()

// Draw paints the region: fill over every cell, an outline of strokeWidth
// px along the dissolved rings (holes included), and label (when not empty)
// at the outline's centroid in labelCol at fontSize.
func (r *Region) Draw(ctx context.Context, p portolan.Projector, h *h3.Handle, cells []uint64, fill, stroke color.Color, strokeWidth float32, label string, labelCol color.Color, fontSize float32) (err error) {
	if len(cells) == 0 {
		return
	}
	if err = r.cells.Cells(ctx, p, h, cells, []color.Color{fill}, color.Color{}, 0); err != nil {
		return
	}
	if err = r.dissolve(ctx, h, cells); err != nil {
		return
	}
	for i := range r.ringLats {
		p.Polyline(r.ringLats[i], r.ringLngs[i], stroke, strokeWidth)
	}
	if label != "" && r.haveLabel {
		p.Label(r.labelAt, 0, 0, 1, 1, label, fontSize, labelCol)
	}
	return
}

// dissolve refreshes the cached outline when the cell set changed.
func (r *Region) dissolve(ctx context.Context, h *h3.Handle, cells []uint64) error {
	var hh maphash.Hash
	hh.SetSeed(regionSeed)
	for _, c := range cells {
		var b [8]byte
		for i := range b {
			b[i] = byte(c >> (8 * i))
		}
		hh.Write(b[:])
	}
	key := hh.Sum64()
	if key == r.key && len(cells) == r.n && r.ringLats != nil {
		return nil
	}
	lats, lngs, ringOffsets, polygonOffsets, err := h.DissolveE(ctx, cells)
	if err != nil {
		return eh.Errorf("h3overlay: dissolve: %w", err)
	}
	r.key, r.n = key, len(cells)
	r.ringLats, r.ringLngs = r.ringLats[:0], r.ringLngs[:0]
	r.haveLabel = false
	largest := 0.0
	for ri := 0; ri+1 < len(ringOffsets); ri++ {
		a, b := int(ringOffsets[ri]), int(ringOffsets[ri+1])
		if b-a < 3 {
			continue
		}
		rl := make([]float64, 0, b-a+1)
		rg := make([]float64, 0, b-a+1)
		rl = append(rl, lats[a:b]...)
		rg = append(rg, lngs[a:b]...)
		rl = append(rl, lats[a])
		rg = append(rg, lngs[a])
		r.ringLats = append(r.ringLats, rl)
		r.ringLngs = append(r.ringLngs, rg)
		// The label goes on the largest exterior ring (the first ring of a
		// polygon), measured by the shoelace area in degrees — a proxy good
		// enough to pick a ring.
		if isExterior(ri, polygonOffsets) {
			if area := shoelace(lats[a:b], lngs[a:b]); area > largest {
				largest = area
				r.labelAt = centroid(lats[a:b], lngs[a:b])
				r.haveLabel = true
			}
		}
	}
	return nil
}

func isExterior(ring int, polygonOffsets []int32) bool {
	for _, off := range polygonOffsets {
		if int(off) == ring {
			return true
		}
	}
	return false
}

func shoelace(lats, lngs []float64) float64 {
	sum := 0.0
	for i := range lats {
		j := (i + 1) % len(lats)
		sum += lngs[i]*lats[j] - lngs[j]*lats[i]
	}
	if sum < 0 {
		sum = -sum
	}
	return sum / 2
}

func centroid(lats, lngs []float64) portolan.LatLng {
	var sl, sg float64
	for i := range lats {
		sl += lats[i]
		sg += lngs[i]
	}
	n := float64(len(lats))
	return portolan.LL(sl/n, sg/n)
}
