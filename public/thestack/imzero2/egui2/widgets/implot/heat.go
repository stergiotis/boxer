package implot

import (
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
)

// heatmapRectCells is the SD5 routing threshold: grids at or under it draw
// as one paintRectsFilled batch, larger grids go through the paintImage
// texture route. The value is a reasoned default; the SD5 measurement pass
// may move it.
const heatmapRectCells = 4096

// heatFrame carries one Heatmap declaration to End's render pass.
type heatFrame struct {
	values         []float64
	rows, cols     int
	cm             *colormap.Config
	x0, y0, x1, y1 float64
}

// heatTex is the retained per-label texture-route state: the colorized
// pixel buffer, the content signature it was built from, and the version
// counter of the mapRaster-style ship-once protocol.
type heatTex struct {
	pix         []uint32
	hash        uint64
	version     uint64
	sentVersion uint64
}

// Heatmap declares a rows×cols grid of values, row-major with row 0 at the
// TOP edge (y1), colorized through the colormap config — the integration
// point with the house `colormap` widget (its palettes, range and log
// normalization apply as-is). Small grids draw as a rect batch; grids past
// heatmapRectCells route through a cached texture per SD5, re-shipping
// pixels only when the content signature changes or the host reports the
// texture starved.
func (p *Plot) Heatmap(label string, values []float64, rows int, cols int, cm *colormap.Config, x0 float64, y0 float64, x1 float64, y1 float64) *Plot {
	p.setupLocked = true
	if !p.st.hidden[label] {
		p.fitX(x0)
		p.fitX(x1)
		p.fitY(y0)
		p.fitY(y1)
	}
	p.series = append(p.series, seriesFrame{kind: kindHeatmap, label: label,
		heat: &heatFrame{values: values, rows: rows, cols: cols, cm: cm, x0: x0, y0: y0, x1: x1, y1: y1}})
	return p
}

// Histogram bins the samples (Sturges' rule when bins <= 0) and declares
// the result as a bar series over the sample range. With density the bar
// heights integrate to one.
func (p *Plot) Histogram(label string, samples []float64, bins int, density bool) *Plot {
	counts, lo, width, n := binSamples(samples, bins, density)
	if n == 0 {
		return p
	}
	centers := make([]float64, len(counts))
	for i := range counts {
		centers[i] = lo + (float64(i)+0.5)*width
	}
	return p.Bars(label, centers, counts, width)
}

// Histogram2D bins (xs, ys) pairs into an xBins×yBins grid and declares it
// as a heatmap over the data extent, colorized by count.
func (p *Plot) Histogram2D(label string, xs []float64, ys []float64, xBins int, yBins int, cm *colormap.Config) *Plot {
	values, x0, x1, y0, y1, ok := bin2D(xs, ys, xBins, yBins)
	if !ok {
		return p
	}
	if cm.DataMax <= cm.DataMin {
		vmax := 0.0
		for _, v := range values {
			vmax = math.Max(vmax, v)
		}
		cm.DataMin, cm.DataMax = 0, math.Max(vmax, 1)
	}
	return p.Heatmap(label, values, yBins, xBins, cm, x0, y0, x1, y1)
}

// binSamples is the shared 1D binning core: NaNs dropped, Sturges default,
// clamp-to-edge on the max sample. Returns per-bin heights (already
// density-normalized when asked), the low edge, the bin width and the
// retained sample count.
func binSamples(samples []float64, bins int, density bool) (counts []float64, lo float64, width float64, n int) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range samples {
		if math.IsNaN(v) {
			continue
		}
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
		n++
	}
	if n == 0 || hi <= lo {
		return nil, 0, 0, 0
	}
	if bins <= 0 {
		bins = int(math.Ceil(math.Log2(float64(n)))) + 1
	}
	counts = make([]float64, bins)
	width = (hi - lo) / float64(bins)
	for _, v := range samples {
		if math.IsNaN(v) {
			continue
		}
		idx := int((v - lo) / width)
		if idx >= bins {
			idx = bins - 1
		}
		counts[idx]++
	}
	if density {
		norm := 1.0 / (float64(n) * width)
		for i := range counts {
			counts[i] *= norm
		}
	}
	return counts, lo, width, n
}

// bin2D is the 2D binning core behind Histogram2D. The returned grid is
// row-major with row 0 at the TOP (the max-y bin), matching Heatmap's
// orientation contract.
func bin2D(xs []float64, ys []float64, xBins int, yBins int) (values []float64, x0, x1, y0, y1 float64, ok bool) {
	n := min(len(xs), len(ys))
	x0, x1 = math.Inf(1), math.Inf(-1)
	y0, y1 = math.Inf(1), math.Inf(-1)
	kept := 0
	for i := range n {
		if math.IsNaN(xs[i]) || math.IsNaN(ys[i]) {
			continue
		}
		x0 = math.Min(x0, xs[i])
		x1 = math.Max(x1, xs[i])
		y0 = math.Min(y0, ys[i])
		y1 = math.Max(y1, ys[i])
		kept++
	}
	if kept == 0 || x1 <= x0 || y1 <= y0 || xBins < 1 || yBins < 1 {
		return nil, 0, 0, 0, 0, false
	}
	values = make([]float64, xBins*yBins)
	xw := (x1 - x0) / float64(xBins)
	yw := (y1 - y0) / float64(yBins)
	for i := range n {
		if math.IsNaN(xs[i]) || math.IsNaN(ys[i]) {
			continue
		}
		xi := int((xs[i] - x0) / xw)
		if xi >= xBins {
			xi = xBins - 1
		}
		yi := int((ys[i] - y0) / yw)
		if yi >= yBins {
			yi = yBins - 1
		}
		row := yBins - 1 - yi // row 0 = top = max-y bin
		values[row*xBins+xi]++
	}
	return values, x0, x1, y0, y1, true
}

// hashF64s is an FNV-1a content signature over the value bits, used by the
// texture route to skip recolorize+reupload for unchanged grids.
func hashF64s(vs []float64) uint64 {
	h := uint64(1469598103934665603)
	for _, v := range vs {
		h ^= math.Float64bits(v)
		h *= 1099511628211
	}
	return h
}

// emitHeatmap renders one heatmap declaration through the SD5 routing.
func (p *Plot) emitHeatmap(s *seriesFrame, tr transform) {
	h := s.heat
	n := h.rows * h.cols
	if n == 0 || len(h.values) < n {
		return
	}
	pxL, pxR := tr.pxX(h.x0), tr.pxX(h.x1)
	pyT, pyB := tr.pxY(h.y1), tr.pxY(h.y0)
	if n <= heatmapRectCells {
		minXs := make([]float32, 0, n)
		minYs := make([]float32, 0, n)
		maxXs := make([]float32, 0, n)
		maxYs := make([]float32, 0, n)
		cols := make([]uint32, 0, n)
		cw := (h.x1 - h.x0) / float64(h.cols)
		rh := (h.y1 - h.y0) / float64(h.rows)
		for r := range h.rows {
			cellTop := h.y1 - float64(r)*rh
			for cix := range h.cols {
				cellL := h.x0 + float64(cix)*cw
				minXs = append(minXs, tr.pxX(cellL))
				minYs = append(minYs, tr.pxY(cellTop))
				maxXs = append(maxXs, tr.pxX(cellL+cw))
				maxYs = append(maxYs, tr.pxY(cellTop-rh))
				cols = append(cols, h.cm.At(h.values[r*h.cols+cix]))
			}
		}
		c.PaintRectsFilled(minXs, minYs, maxXs, maxYs, color.ColorsFromU32(cols)).Send()
		return
	}

	// Texture route: colorize into the retained per-label buffer only when
	// the content signature moves; ship pixels only on a version change or
	// when the host reports the texture starved (hidden-tab discard, LRU).
	if p.st.heatCache == nil {
		p.st.heatCache = make(map[string]*heatTex, 2)
	}
	ht := p.st.heatCache[s.label]
	if ht == nil {
		ht = &heatTex{}
		p.st.heatCache[s.label] = ht
	}
	hash := hashF64s(h.values[:n])
	if hash != ht.hash || len(ht.pix) != n {
		if cap(ht.pix) < n {
			ht.pix = make([]uint32, n)
		}
		ht.pix = ht.pix[:n]
		for i := range n {
			ht.pix[i] = h.cm.At(h.values[i])
		}
		ht.hash = hash
		ht.version++
	}
	texId := p.ids.PrepareStr("heat-" + s.label).Derive()
	sm := c.CurrentApplicationState.StateManager
	send := ht.pix
	if ht.version == ht.sentVersion && !sm.TextureStarved(texId) {
		send = emptyPixels // unchanged → reuse the cached texture
	}
	c.PaintImage(texId, pxL, pyT, pxR, pyB, uint32(h.cols), uint32(h.rows), ht.version, send).
		Nearest(true).
		Send()
	ht.sentVersion = ht.version
}

// emptyPixels is the non-nil empty slice the ship-once protocol sends for
// an unchanged texture (nil would read as a different wire shape).
var emptyPixels = []uint32{}
