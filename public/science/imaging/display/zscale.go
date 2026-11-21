package display

import (
	"iter"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// ZScaleContrast is the contrast zscale uses when none is given: the slope
// of the fitted line is divided by it, so a smaller contrast widens the
// limits.
const ZScaleContrast = 0.25

// ZScaleSamples is the number of grid points zscale reads when no number is
// given.
const ZScaleSamples = 1000

const (
	// zscaleKRej is how many standard deviations of the residuals a sample
	// may lie off the fitted line before it is rejected.
	zscaleKRej = 2.5
	// zscaleMaxIterations bounds the rounds of fitting and rejecting.
	zscaleMaxIterations = 5
	// zscaleMinSamples is the fewest samples a line is fitted to.
	zscaleMinSamples = 5
)

// ZScaleLimits finds a plane's limits by zscale (ADR-0401 §SD7) from its
// values — the stored samples through the value transform — read at the
// points of a regular grid; contrast and samples of 0 take [ZScaleContrast]
// and [ZScaleSamples]. It reports ok false for a negative or non-finite
// contrast, a negative sample count, a plane with no valid sample on the
// grid, and limits that are not a range (a plane whose sampled values are
// one value, or a fit with no slope). The same plane always gives the same
// limits. The steps are those of [ZScaleLimitsAt].
func ZScaleLimits(plane imaging.Plane, contrast float64, samples int) (limits Limits, ok bool) {
	return ZScaleLimitsAt(plane.Width(), plane.Height(), plane.ValueAt, contrast, samples)
}

// ZScaleLimitsAt is [ZScaleLimits] over any source of values: a grid
// width × height whose value at (col, row) at returns, valid false for an
// invalid sample. A tiled store reads the grid's points through it. at is
// called once per grid point, in rows from the top, each row from the
// left.
//
// The steps:
//
//  1. The grid. n is the sample count asked for. The step d is the smallest
//     positive integer for which ⌈w/d⌉·⌈h/d⌉ ≤ n; the image is cut into
//     nx = ⌈w/d⌉ columns and ny = ⌈h/d⌉ rows of cells of near-equal size,
//     and each cell gives the pixel at its centre: column ⌊(2i+1)·w/(2nx)⌋,
//     row ⌊(2j+1)·h/(2ny)⌋. An image of at most n pixels gives every pixel.
//     An invalid sample, and an infinite value, is skipped, not replaced, so
//     the sample holds the finite valid values at the grid's points, m of
//     them.
//  2. The values are sorted, ascending; their median is the middle value,
//     or the mean of the two middle ones.
//  3. A line value = a + b·i is fitted by least squares to the sorted values
//     against their index i = 0 … m−1, over the samples not rejected. The
//     residuals of all samples are taken from it, and a sample whose
//     residual lies more than 2.5 standard deviations (of the residuals of
//     the samples not rejected, taken over them as a population) from zero
//     is rejected. Each rejected sample also rejects the ⌊(g−1)/2⌋ samples
//     before it and the g−1−⌊(g−1)/2⌋ after it, g = max(1, ⌊m/100⌋). A
//     rejected sample is never taken back.
//  4. Step 3 repeats, at most five times, while the last round rejected
//     something and at least max(5, ⌊m/2⌋) samples remain. The slope b is
//     that of the last line fitted.
//  5. When fewer than max(5, ⌊m/2⌋) samples remain — or m is below 5, and
//     no line was fitted — the limits are the smallest and largest sampled
//     value. Otherwise, with c = ⌊(m−1)/2⌋ the index of the centre sample,
//     Lo = median − c·b/contrast and Hi = median + (m−1−c)·b/contrast: the
//     line through the median at the centre, steepened by 1/contrast,
//     evaluated at the ends of the sample. Each is cut to the sampled
//     values' extremes.
//
// The rejection and its constants are those of the zscale of IRAF's display
// task as astropy's ZScaleInterval documents them (krej 2.5, five
// iterations, at most half the sample rejected, a line fitted to at least
// five). The ends in step 5 are IRAF's: the centre sample is c samples from
// the first and m−1−c from the last.
func ZScaleLimitsAt(width, height int, at func(col, row int) (value float64, valid bool), contrast float64, samples int) (limits Limits, ok bool) {
	if contrast == 0 {
		contrast = ZScaleContrast
	}
	if samples == 0 {
		samples = ZScaleSamples
	}
	if !(contrast > 0) || math.IsInf(contrast, 0) || samples < 0 || samples > math.MaxInt32 || width <= 0 || height <= 0 {
		return limits, false
	}
	values := zscaleSample(width, height, samples, at)
	if len(values) == 0 {
		return limits, false
	}
	slices.Sort(values)
	fit := fitZScale(values)
	lo, hi := fit.limits(contrast)
	if !(hi > lo) {
		return limits, false
	}
	return Limits{Lo: lo, Hi: hi, Source: LimitsSourceZScale, ZScaleContrast: contrast, ZScaleSamples: int32(samples)}, true
}

// zscaleGrid is the grid's step: the columns and the rows of the cells
// whose centres are read.
func zscaleGrid(width, height, samples int) (nx, ny int) {
	if int64(width)*int64(height) <= int64(samples) {
		return width, height
	}
	d := max(1, int(math.Sqrt(float64(width)*float64(height)/float64(samples))))
	for {
		nx, ny = (width+d-1)/d, (height+d-1)/d
		if int64(nx)*int64(ny) <= int64(samples) {
			return nx, ny
		}
		d++
	}
}

// ZScaleGrid is the pixels zscale reads of a width × height image for a
// sample count (step 1 of [ZScaleLimitsAt]), as (col, row), in the order
// [ZScaleLimitsAt] calls its at: rows from the top, each row from the left.
// A count of 0 takes [ZScaleSamples]. A reader of a tiled image collects
// them first to read each tile they fall in once. A negative count or an
// empty image has no points.
func ZScaleGrid(width, height, samples int) iter.Seq2[int, int] {
	return func(yield func(col, row int) bool) {
		if samples == 0 {
			samples = ZScaleSamples
		}
		if samples < 0 || width <= 0 || height <= 0 {
			return
		}
		nx, ny := zscaleGrid(width, height, samples)
		for j := range ny {
			row := int((2*int64(j) + 1) * int64(height) / (2 * int64(ny)))
			for i := range nx {
				if !yield(int((2*int64(i)+1)*int64(width)/(2*int64(nx))), row) {
					return
				}
			}
		}
	}
}

// zscaleSample reads the valid values at the grid's points.
func zscaleSample(width, height, samples int, at func(col, row int) (value float64, valid bool)) (values []float64) {
	nx, ny := zscaleGrid(width, height, samples)
	values = make([]float64, 0, nx*ny)
	for col, row := range ZScaleGrid(width, height, samples) {
		if v, valid := at(col, row); valid && !math.IsNaN(v) && !math.IsInf(v, 0) {
			values = append(values, v)
		}
	}
	return values
}

// zscaleFit is what the rejection loop leaves: the sample's extremes and
// median, the centre index, and the slope of the last line fitted when
// enough samples remained to use it.
type zscaleFit struct {
	m              int
	lo, hi, median float64
	slope          float64
	fitted         bool
}

// fitZScale runs steps 3 and 4 of [ZScaleLimitsAt] over sorted values.
func fitZScale(sorted []float64) (fit zscaleFit) {
	m := len(sorted)
	fit.m, fit.lo, fit.hi = m, sorted[0], sorted[m-1]
	if m%2 == 1 {
		fit.median = sorted[m/2]
	} else {
		fit.median = (sorted[m/2-1] + sorted[m/2]) / 2
	}
	minGood := max(zscaleMinSamples, m/2)
	grow := max(1, m/100)
	before := (grow - 1) / 2
	after := grow - 1 - before
	rejected := make([]bool, m)
	hit := make([]bool, m)
	good, last := m, m+1
	var slope float64
	var didFit bool
	for range zscaleMaxIterations {
		if good >= last || good < minGood {
			break
		}
		a, b := fitLine(sorted, rejected)
		slope, didFit = b, true
		// The standard deviation of the residuals of the samples kept.
		var mean float64
		for i, v := range sorted {
			if !rejected[i] {
				mean += v - (a + b*float64(i))
			}
		}
		mean /= float64(good)
		var ss float64
		for i, v := range sorted {
			if !rejected[i] {
				d := v - (a + b*float64(i)) - mean
				ss += d * d
			}
		}
		threshold := zscaleKRej * math.Sqrt(ss/float64(good))
		clear(hit)
		for i, v := range sorted {
			r := v - (a + b*float64(i))
			hit[i] = rejected[i] || r < -threshold || r > threshold
		}
		// Grow every rejection into its neighbourhood.
		for i := range sorted {
			if hit[i] {
				for k := max(0, i-before); k <= min(m-1, i+after); k++ {
					rejected[k] = true
				}
			}
		}
		last, good = good, 0
		for _, r := range rejected {
			if !r {
				good++
			}
		}
	}
	if didFit && good >= minGood {
		fit.slope, fit.fitted = slope, true
	}
	return fit
}

// fitLine is the least-squares line value = a + b·i through the sorted
// values not rejected, against their index; at least two of them are kept
// (zscaleMinSamples).
func fitLine(sorted []float64, rejected []bool) (a, b float64) {
	var n, sx, sy float64
	for i, v := range sorted {
		if !rejected[i] {
			n++
			sx += float64(i)
			sy += v
		}
	}
	mx, my := sx/n, sy/n
	var sxx, sxy float64
	for i, v := range sorted {
		if !rejected[i] {
			dx := float64(i) - mx
			sxx += dx * dx
			sxy += dx * (v - my)
		}
	}
	b = sxy / sxx
	return my - b*mx, b
}

// limits is step 5 of [ZScaleLimitsAt].
func (inst zscaleFit) limits(contrast float64) (lo, hi float64) {
	if !inst.fitted {
		return inst.lo, inst.hi
	}
	c := (inst.m - 1) / 2
	step := inst.slope / contrast
	lo = max(inst.lo, inst.median-float64(c)*step)
	hi = min(inst.hi, inst.median+float64(inst.m-1-c)*step)
	return lo, hi
}
