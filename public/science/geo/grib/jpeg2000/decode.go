package jpeg2000

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Image is a decoded single-component image: Samples holds Width × Height
// values in row-major order, DC level shifted and clamped to the
// component's precision; Precision and Signed are the codestream's.
type Image struct {
	Width, Height int
	Precision     uint8
	Signed        bool
	Samples       []int32
}

// Decode decodes a raw codestream or a JP2 file whose codestream fits the
// profile the package documents.
func Decode(cs []byte) (img Image, err error) {
	h, err := parseHeaderE(cs)
	if err != nil {
		return
	}
	err = h.checkProfileE()
	if err != nil {
		return
	}
	res, err := h.layoutE()
	if err != nil {
		return
	}
	err = h.readPacketsE(res)
	if err != nil {
		return
	}
	// Tier-1: every included code-block into its sub-band.
	var t1 t1Decoder
	for _, rs := range res {
		for _, b := range rs.bands {
			for pi := range b.precincts {
				for i := range b.precincts[pi].blocks {
					cb := &b.precincts[pi].blocks[i]
					if !cb.included || cb.passes == 0 {
						continue
					}
					planes := b.mb - cb.zeroPlanes
					if planes < 0 || planes > 31 {
						err = eb.Build().Int32("mb", b.mb).Int32("zeroPlanes", cb.zeroPlanes).Errorf("bit-plane count: %w", ErrCorrupt)
						return
					}
					t1.decodeBlock(cb, b, planes, cb.passes, h.cod.cbStyle)
				}
			}
		}
	}
	// Inverse wavelet, level by level from the coarsest.
	nl := int(h.cod.levels)
	var scratch []int32
	ll := res[0].bands[0]
	current := ll.coeffs
	cx0, cy0, cx1, cy1 := ll.x0, ll.y0, ll.x1, ll.y1
	for r := 1; r <= nl; r++ {
		rs := res[r]
		hl, lh, hh := rs.bands[0], rs.bands[1], rs.bands[2]
		lower := &subBand{kind: bandKindLL, x0: cx0, y0: cy0, x1: cx1, y1: cy1, coeffs: current}
		out := make([]int32, int(rs.x1-rs.x0)*int(rs.y1-rs.y0))
		sr2D(lower, hl, lh, hh, rs.x0, rs.x1, rs.y0, rs.y1, out, &scratch)
		current = out
		cx0, cy0, cx1, cy1 = rs.x0, rs.y0, rs.x1, rs.y1
	}
	// DC level shift and clamp (Annex G).
	img.Width = int(h.width - h.x0)
	img.Height = int(h.height - h.y0)
	img.Precision = h.precision
	img.Signed = h.signed
	img.Samples = current
	if !h.signed {
		shift := int32(1) << (h.precision - 1)
		maxV := int32(1)<<h.precision - 1
		for i, v := range img.Samples {
			v += shift
			if v < 0 {
				v = 0
			} else if v > maxV {
				v = maxV
			}
			img.Samples[i] = v
		}
	}
	return
}
