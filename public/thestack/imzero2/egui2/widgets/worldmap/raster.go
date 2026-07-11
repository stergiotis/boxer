package worldmap

import (
	"math"
	"sort"
)

// Software rasterization of the atlas into an RGBA texture + a country-index
// hit-test buffer (ADR-0114 §SD3). Scanline even-odd filling handles the
// concave, holed, multi-part country outlines uniformly — the reason this is
// rasterized Go-side at all instead of using egui's convex-only polygon fill.
//
// The pass renders at ssFactor× supersampling and box-downsamples, baking
// country borders (drawn into a 2× coverage mask) into the same buffer. The
// output is row-major 0xRRGGBBAA, row 0 = north — the Image widget's pixel
// contract. The index buffer is at the output (1×) resolution: the Image
// hover readout is in texture-pixel space, so hit-testing is one array load.

const ssFactor = 2

// rasterStyle carries the per-render colors. fills is indexed by CountryIdx
// and must cover every country in the atlas.
type rasterStyle struct {
	fills  []uint32 // per-country fill, 0xRRGGBBAA
	sea    uint32   // background (alpha 0 lets the pane background show)
	stroke uint32   // border color; alpha scales the baked stroke opacity
}

// rasterize renders the atlas at (w × h). Returns the RGBA pixels and the
// per-pixel country index (NoCountry = sea).
func rasterize(atlas *Atlas, w, h int, style rasterStyle) (rgba []uint32, index []CountryIdx) {
	w2 := w * ssFactor
	h2 := h * ssFactor
	idx2 := make([]CountryIdx, w2*h2)
	for i := range idx2 {
		idx2[i] = NoCountry
	}
	var xs []float64 // scanline crossing scratch, reused across rings
	for ci := range atlas.Countries {
		xs = fillCountry(idx2, w2, h2, &atlas.Countries[ci], CountryIdx(ci), xs)
	}
	mask2 := make([]uint8, w2*h2)
	for ci := range atlas.Countries {
		strokeCountry(mask2, w2, h2, &atlas.Countries[ci])
	}

	rgba = make([]uint32, w*h)
	index = make([]CountryIdx, w*h)
	sr, sg, sb, sa := unpackRGBA(style.stroke)
	for y := range h {
		for x := range w {
			// Gather the ssFactor² subsamples.
			var r, g, b, a, cover uint32
			var cand [ssFactor * ssFactor]CountryIdx
			n := 0
			for sy := range ssFactor {
				row := (y*ssFactor + sy) * w2
				for sx := range ssFactor {
					o := row + x*ssFactor + sx
					ci := idx2[o]
					cand[n] = ci
					n++
					col := style.sea
					if ci != NoCountry {
						col = style.fills[ci]
					}
					cr, cg, cb, ca := unpackRGBA(col)
					r += cr
					g += cg
					b += cb
					a += ca
					cover += uint32(mask2[o])
				}
			}
			const nSub = ssFactor * ssFactor
			r /= nSub
			g /= nSub
			b /= nSub
			a /= nSub
			// Blend the border stroke over the averaged fill, scaled by the
			// subsample coverage — 4-level anti-aliasing at ssFactor 2.
			if cover > 0 {
				ba := sa * cover / nSub
				r = (sr*ba + r*(255-ba)) / 255
				g = (sg*ba + g*(255-ba)) / 255
				b = (sb*ba + b*(255-ba)) / 255
				if ba > a {
					a = ba
				}
			}
			o := y*w + x
			rgba[o] = r<<24 | g<<16 | b<<8 | a
			index[o] = majorityIdx(cand[:])
		}
	}
	return
}

// majorityIdx picks the hit-test index for one output pixel from its
// subsamples: the most frequent value, non-sea preferred on ties, then the
// lower index — deterministic, and biased toward reporting *a* country on
// boundary pixels (hover feel beats sub-pixel pedantry here).
func majorityIdx(cand []CountryIdx) CountryIdx {
	best := NoCountry
	bestCount := 0
	for i, ci := range cand {
		count := 1
		for _, cj := range cand[i+1:] {
			if cj == ci {
				count++
			}
		}
		switch {
		case count > bestCount:
			best, bestCount = ci, count
		case count == bestCount && best == NoCountry && ci != NoCountry:
			best = ci
		case count == bestCount && ci != NoCountry && best != NoCountry && ci < best:
			best = ci
		}
	}
	return best
}

// fillCountry scanline-fills one country's rings into the index buffer using
// the even-odd rule with half-open edges (y1 <= yc < y2), sampling at pixel
// centers. Later countries overwrite earlier ones; upstream admin-0 features
// are disjoint so ordering is immaterial.
func fillCountry(idx2 []CountryIdx, w2, h2 int, ct *Country, ci CountryIdx, xs []float64) []float64 {
	yLo := int(math.Floor(float64(ct.bbox[1])*float64(h2) - 0.5))
	yHi := int(math.Ceil(float64(ct.bbox[3])*float64(h2) - 0.5))
	if yLo < 0 {
		yLo = 0
	}
	if yHi > h2-1 {
		yHi = h2 - 1
	}
	fw := float64(w2)
	fh := float64(h2)
	for y := yLo; y <= yHi; y++ {
		yc := float64(y) + 0.5
		xs = xs[:0]
		for _, ring := range ct.rings {
			for i := 1; i < len(ring); i++ {
				y1 := float64(ring[i-1].Y) * fh
				y2 := float64(ring[i].Y) * fh
				if (y1 <= yc) == (y2 <= yc) { // both above or both below
					continue
				}
				x1 := float64(ring[i-1].X) * fw
				x2 := float64(ring[i].X) * fw
				xs = append(xs, x1+(yc-y1)*(x2-x1)/(y2-y1))
			}
		}
		if len(xs) < 2 {
			continue
		}
		sort.Float64s(xs)
		row := y * w2
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(math.Ceil(xs[i] - 0.5))
			x1 := int(math.Ceil(xs[i+1] - 0.5))
			if x0 < 0 {
				x0 = 0
			}
			if x1 > w2 {
				x1 = w2
			}
			for x := x0; x < x1; x++ {
				idx2[row+x] = ci
			}
		}
	}
	return xs
}

// strokeCountry marks the country's ring outlines in the supersampled
// coverage mask (a DDA line walk, 1 subpixel wide — ~0.5 output px, softened
// by the downsample). Shared borders are marked by both neighbours onto the
// same subpixels, so they don't double-darken.
func strokeCountry(mask2 []uint8, w2, h2 int, ct *Country) {
	fw := float64(w2)
	fh := float64(h2)
	for _, ring := range ct.rings {
		for i := 1; i < len(ring); i++ {
			x0 := float64(ring[i-1].X) * fw
			y0 := float64(ring[i-1].Y) * fh
			x1 := float64(ring[i].X) * fw
			y1 := float64(ring[i].Y) * fh
			steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
			for s := 0; s <= steps; s++ {
				t := float64(s) / float64(steps)
				x := int(x0 + t*(x1-x0))
				y := int(y0 + t*(y1-y0))
				if x < 0 || x >= w2 || y < 0 || y >= h2 {
					continue
				}
				mask2[y*w2+x] = 1
			}
		}
	}
}

func unpackRGBA(c uint32) (r, g, b, a uint32) {
	return c >> 24, (c >> 16) & 0xff, (c >> 8) & 0xff, c & 0xff
}
