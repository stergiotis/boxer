package worldmap

import (
	"math"
	"sort"
)

// Software rasterization of the atlas into an RGBA texture + a country-index
// hit-test buffer (ADR-0114 §SD3). Scanline even-odd filling handles the
// concave, holed, multi-part country outlines uniformly — the reason this is
// rasterized Go-side at all instead of using egui's polygon fill.
//
// The pass renders at ssFactor× supersampling and box-downsamples, baking
// country borders (drawn into a 2× coverage mask) into the same buffer. The
// output is row-major 0xRRGGBBAA, row 0 = north — the image pixel contract.
// The index buffer is at the output (1×) resolution: hover maps the canvas
// pointer to a texture pixel, so hit-testing is one array load.
//
// The pass is split in two, because the expensive half does not depend on the
// data. rasterGeometry holds everything derived from the outlines and the
// output size; resolve turns it into pixels for a given set of fills. A value
// change re-runs only the second half, which is a lookup per pixel plus a
// fix-up over the boundary pixels — the scanline fill, the border walk and the
// per-pixel subsample majority all stay cached.

const (
	ssFactor = 2
	nSub     = ssFactor * ssFactor
	// coverLevels is the number of distinct border-coverage values a pixel can
	// carry: none through all nSub subsamples.
	coverLevels = nSub + 1
)

// rasterStyle carries the per-render colors. fills is indexed by CountryIdx
// and must cover every country in the atlas.
type rasterStyle struct {
	fills  []uint32 // per-country fill, 0xRRGGBBAA
	sea    uint32   // background (alpha 0 lets the pane background show)
	stroke uint32   // border color; alpha scales the baked stroke opacity
}

// mixedPixel is one output pixel whose subsamples disagree — a coastline or a
// shared border. Its color is the mean of the four subsample colors, so it
// cannot come from the fill lookup table and is fixed up separately.
type mixedPixel struct {
	off int32
	sub [nSub]CountryIdx
}

// rasterGeometry is the size-dependent half of the pass: what the outlines and
// the output size determine, and the data does not. Built once per size and
// reused across every value, palette and column change.
type rasterGeometry struct {
	w, h int
	// index is the per-pixel country — the hit-test buffer, and the fill key
	// for every pixel whose subsamples agree.
	index []CountryIdx
	// cover is the per-pixel border coverage, 0..nSub.
	cover []uint8
	// mixed are the boundary pixels, in scan order.
	mixed []mixedPixel
}

// buildRasterGeometry runs the scanline fill, the border walk and the
// subsample reduction for an (w × h) output.
func buildRasterGeometry(atlas *Atlas, w, h int) *rasterGeometry {
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

	inst := &rasterGeometry{
		w:     w,
		h:     h,
		index: make([]CountryIdx, w*h),
		cover: make([]uint8, w*h),
	}
	for y := range h {
		for x := range w {
			// Gather the ssFactor² subsamples.
			var cand [nSub]CountryIdx
			var cover uint8
			n := 0
			uniform := true
			for sy := range ssFactor {
				row := (y*ssFactor + sy) * w2
				for sx := range ssFactor {
					o := row + x*ssFactor + sx
					ci := idx2[o]
					cand[n] = ci
					if ci != cand[0] {
						uniform = false
					}
					n++
					cover += mask2[o]
				}
			}
			o := y*w + x
			inst.cover[o] = cover
			if uniform {
				// majorityIdx of nSub identical values is that value.
				inst.index[o] = cand[0]
				continue
			}
			inst.index[o] = majorityIdx(cand[:])
			inst.mixed = append(inst.mixed, mixedPixel{off: int32(o), sub: cand})
		}
	}
	return inst
}

// resolve paints the geometry into rgba (which must hold w*h pixels) for the
// given style. Every pixel takes its color from the fill table; the boundary
// pixels are then overwritten with their four-subsample mean.
func (inst *rasterGeometry) resolve(rgba []uint32, style rasterStyle) {
	lut := buildFillLUT(style)
	for o, ci := range inst.index {
		rgba[o] = lut[(int(ci)+1)*coverLevels+int(inst.cover[o])]
	}
	sr, sg, sb, sa := unpackRGBA(style.stroke)
	for i := range inst.mixed {
		mp := &inst.mixed[i]
		var r, g, b, a uint32
		for _, ci := range mp.sub {
			cr, cg, cb, ca := unpackRGBA(fillOf(style, ci))
			r += cr
			g += cg
			b += cb
			a += ca
		}
		r /= nSub
		g /= nSub
		b /= nSub
		a /= nSub
		r, g, b, a = blendStroke(r, g, b, a, uint32(inst.cover[mp.off]), sr, sg, sb, sa)
		rgba[mp.off] = r<<24 | g<<16 | b<<8 | a
	}
}

// buildFillLUT precomputes the final color of every (fill, coverage) pair: one
// row per country plus the sea at row 0, one column per coverage level. A
// pixel whose subsamples agree averages to its own fill exactly, so its color
// is a table lookup rather than four unpacks, a divide and a blend.
func buildFillLUT(style rasterStyle) []uint32 {
	lut := make([]uint32, (len(style.fills)+1)*coverLevels)
	sr, sg, sb, sa := unpackRGBA(style.stroke)
	for i := range len(style.fills) + 1 {
		col := style.sea
		if i > 0 {
			col = style.fills[i-1]
		}
		cr, cg, cb, ca := unpackRGBA(col)
		for cover := range coverLevels {
			r, g, b, a := blendStroke(cr, cg, cb, ca, uint32(cover), sr, sg, sb, sa)
			lut[i*coverLevels+cover] = r<<24 | g<<16 | b<<8 | a
		}
	}
	return lut
}

// blendStroke lays the border color over an already-averaged fill, scaled by
// the subsample coverage — nSub+1 level anti-aliasing.
func blendStroke(r, g, b, a, cover, sr, sg, sb, sa uint32) (uint32, uint32, uint32, uint32) {
	if cover == 0 {
		return r, g, b, a
	}
	ba := sa * cover / nSub
	r = (sr*ba + r*(255-ba)) / 255
	g = (sg*ba + g*(255-ba)) / 255
	b = (sb*ba + b*(255-ba)) / 255
	if ba > a {
		a = ba
	}
	return r, g, b, a
}

func fillOf(style rasterStyle, ci CountryIdx) uint32 {
	if ci == NoCountry {
		return style.sea
	}
	return style.fills[ci]
}

// rasterize renders the atlas at (w × h) in one shot, building the geometry and
// throwing it away. Callers that re-render on data changes should keep a
// rasterGeometry and call resolve instead.
func rasterize(atlas *Atlas, w, h int, style rasterStyle) (rgba []uint32, index []CountryIdx) {
	g := buildRasterGeometry(atlas, w, h)
	rgba = make([]uint32, w*h)
	g.resolve(rgba, style)
	return rgba, g.index
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
