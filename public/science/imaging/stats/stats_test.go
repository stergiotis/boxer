package stats

import (
	"image"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/imaging"
)

func TestAnIntegerHistogramCountsEverySampleExactly(t *testing.T) {
	p, err := imaging.NewPlaneU16(5, 1, []uint16{7, 7, 300, 65535, 7})
	require.NoError(t, err)
	h := Of(p)
	assert.True(t, h.IsExact())
	assert.Zero(t, h.Count(-1))
	assert.Zero(t, h.Count(65536), "a bin that does not exist")
	_, _, ok := Histogram{}.StoredRange()
	assert.False(t, ok)
	assert.Equal(t, 65536, h.Bins())
	assert.EqualValues(t, 3, h.Count(7))
	assert.EqualValues(t, 1, h.Count(300))
	assert.EqualValues(t, 1, h.Count(65535))
	assert.EqualValues(t, 5, h.Valid())
	assert.Zero(t, h.Invalid())
	lo, hi, okR := h.StoredRange()
	assert.True(t, okR)
	assert.Equal(t, 7.0, lo)
	assert.Equal(t, 65535.0, hi)
	assert.Equal(t, 1.0, h.BinWidth())

	q, _ := imaging.NewPlaneU8(3, 1, []uint8{0, 255, 9})
	h8 := Of(q)
	assert.Equal(t, 256, h8.Bins())
	lo, hi, ok = h8.Range()
	assert.True(t, ok)
	assert.Equal(t, 0.0, lo)
	assert.Equal(t, 255.0, hi)
}

// A percentile of an integer plane is the nearest-rank percentile of its
// sorted samples: a value that occurs, with at least p percent at or below.
func TestAnIntegerPercentileIsTheNearestRank(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		samples := rapid.SliceOfN(rapid.Uint16(), 1, 400).Draw(t, "samples")
		p, err := imaging.NewPlaneU16(len(samples), 1, slices.Clone(samples))
		require.NoError(t, err)
		h := Of(p)
		sorted := slices.Clone(samples)
		slices.Sort(sorted)
		pct := rapid.Float64Range(0, 100).Draw(t, "p")
		got, ok := h.PercentileStored(pct)
		require.True(t, ok)
		rank := int(math.Ceil(pct / 100 * float64(len(sorted))))
		want := float64(sorted[max(rank-1, 0)])
		require.Equal(t, want, got, "p=%v of %d samples", pct, len(sorted))
	})
}

func TestPercentilesAtTheEndsAndOutsideThem(t *testing.T) {
	p, _ := imaging.NewPlaneU8(4, 1, []uint8{10, 20, 30, 40})
	h := Of(p)
	for pct, want := range map[float64]float64{0: 10, 25: 10, 26: 20, 50: 20, 75: 30, 99: 40, 100: 40} {
		got, ok := h.PercentileStored(pct)
		assert.True(t, ok)
		assert.Equal(t, want, got, "p=%v", pct)
	}
	_, ok := h.PercentileStored(-1)
	assert.False(t, ok)
	_, ok = h.PercentileStored(100.5)
	assert.False(t, ok)
	_, ok = h.PercentileStored(math.NaN())
	assert.False(t, ok)
	_, ok = Histogram{}.PercentileStored(50)
	assert.False(t, ok)
	_, _, ok = Histogram{}.Range()
	assert.False(t, ok)
}

func TestAFloatHistogramLeavesInvalidSamplesOutAndIsRightToABin(t *testing.T) {
	nan := float32(math.NaN())
	n := 10_000
	s := make([]float32, n+2)
	for i := range n {
		s[i] = float32(i) / float32(n-1) * 50 // 0 … 50, evenly
	}
	s[n], s[n+1] = nan, nan
	p, err := imaging.NewPlaneF32(len(s), 1, s)
	require.NoError(t, err)
	h := Of(p)
	assert.False(t, h.IsExact())
	assert.EqualValues(t, n, h.Valid())
	assert.EqualValues(t, 2, h.Invalid())
	lo, hi, _ := h.StoredRange()
	assert.Equal(t, 0.0, lo)
	assert.Equal(t, 50.0, hi)
	assert.InDelta(t, 50.0/FloatBins, h.BinWidth(), 1e-12)
	var total uint64
	for i := range h.Bins() {
		total += h.Count(i)
	}
	assert.EqualValues(t, n, total, "the largest sample is in the last bin, not past it")
	for _, pct := range []float64{1, 25, 50, 90, 99.5} {
		got, ok := h.PercentileStored(pct)
		require.True(t, ok)
		assert.InDelta(t, pct/100*50, got, 2*h.BinWidth(), "p=%v", pct)
	}

	flat, _ := imaging.NewPlaneF32(3, 1, []float32{2.5, 2.5, nan})
	hf := Of(flat)
	assert.EqualValues(t, 2, hf.Valid())
	got, ok := hf.PercentileStored(50)
	assert.True(t, ok)
	assert.Equal(t, 2.5, got)

	none, _ := imaging.NewPlaneF32(2, 1, []float32{nan, nan})
	hn := Of(none)
	assert.Zero(t, hn.Valid())
	assert.EqualValues(t, 2, hn.Invalid())
	_, ok = hn.PercentileStored(50)
	assert.False(t, ok)
}

func TestValuesGoThroughThePlanesTransform(t *testing.T) {
	p, _ := imaging.NewPlaneU8(3, 1, []uint8{10, 20, 30})
	p, err := p.WithTransform(imaging.ValueTransform{Scale: -2, Offset: 100})
	require.NoError(t, err)
	h := Of(p)
	lo, hi, ok := h.Range()
	assert.True(t, ok)
	assert.Equal(t, 40.0, lo, "the range is in values, lower first")
	assert.Equal(t, 80.0, hi)
	assert.Equal(t, 60.0, h.Value(20))
}

// A region's histogram and moments are those of the samples inside it and
// of no others, for every sample kind.
func TestARegionIsCountedOverItsOwnSamples(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 30).Draw(t, "w")
		h := rapid.IntRange(1, 30).Draw(t, "h")
		raw := rapid.SliceOfN(rapid.Uint16(), w*h, w*h).Draw(t, "samples")
		var p imaging.Plane
		var err error
		switch kind {
		case imaging.SampleKindU8:
			s := make([]uint8, w*h)
			for i := range s {
				s[i] = uint8(raw[i])
			}
			p, err = imaging.NewPlaneU8(w, h, s)
		case imaging.SampleKindU16:
			p, err = imaging.NewPlaneU16(w, h, slices.Clone(raw))
		default:
			s := make([]float32, w*h)
			for i := range s {
				s[i] = float32(raw[i])/9 - 2000
				if raw[i]%7 == 0 {
					s[i] = float32(math.NaN())
				}
			}
			p, err = imaging.NewPlaneF32(w, h, s)
		}
		require.NoError(t, err)
		x0 := rapid.IntRange(0, w-1).Draw(t, "x0")
		y0 := rapid.IntRange(0, h-1).Draw(t, "y0")
		rect := image.Rect(x0, y0, rapid.IntRange(x0+1, w).Draw(t, "x1"), rapid.IntRange(y0+1, h).Draw(t, "y1"))

		got, err := OfRegion(p, rect)
		require.NoError(t, err)
		var vals []float64
		invalid := 0
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				if v, ok := p.StoredAt(x, y); ok {
					vals = append(vals, v)
				} else {
					invalid++
				}
			}
		}
		require.EqualValues(t, len(vals), got.Valid())
		require.EqualValues(t, invalid, got.Invalid())
		mean, okMean := got.Mean()
		sd, okSd := got.StdDev()
		require.Equal(t, len(vals) > 0, okMean)
		require.Equal(t, len(vals) > 0, okSd)
		if len(vals) == 0 {
			return
		}
		var sum float64
		for _, v := range vals {
			sum += v
		}
		wantMean := sum / float64(len(vals))
		var m2 float64
		for _, v := range vals {
			m2 += (v - wantMean) * (v - wantMean)
		}
		require.InDelta(t, wantMean, mean, 1e-6*(1+math.Abs(wantMean)))
		require.InDelta(t, math.Sqrt(m2/float64(len(vals))), sd, 1e-6*(1+sd))
		lo, hi, _ := got.StoredRange()
		require.Equal(t, slices.Min(vals), lo)
		require.Equal(t, slices.Max(vals), hi)
		var total uint64
		for i := range got.Bins() {
			total += got.Count(i)
		}
		require.EqualValues(t, len(vals), total)
	})
}

func TestARegionOutsideThePlaneIsRefusedAndMomentsFollowTheTransform(t *testing.T) {
	p, _ := imaging.NewPlaneU8(4, 2, []uint8{1, 2, 3, 4, 5, 6, 7, 8})
	_, err := OfRegion(p, image.Rect(0, 0, 5, 2))
	assert.Error(t, err)
	_, err = OfRegion(p, image.Rectangle{})
	assert.Error(t, err)

	h, err := OfRegion(p, image.Rect(1, 0, 3, 2)) // 2, 3, 6, 7
	require.NoError(t, err)
	mean, _ := h.Mean()
	sd, _ := h.StdDev()
	assert.Equal(t, 4.5, mean)
	assert.InDelta(t, math.Sqrt((2.5*2.5*2+1.5*1.5*2)/4), sd, 1e-12)
	med, ok := h.Percentile(50)
	assert.True(t, ok)
	assert.Equal(t, 3.0, med, "the nearest-rank median of 2, 3, 6, 7")

	q, err := p.WithTransform(imaging.ValueTransform{Scale: -2, Offset: 10})
	require.NoError(t, err)
	hq, _ := OfRegion(q, image.Rect(1, 0, 3, 2))
	meanQ, _ := hq.Mean()
	sdQ, _ := hq.StdDev()
	assert.Equal(t, 1.0, meanQ)
	assert.InDelta(t, 2*sd, sdQ, 1e-12, "a standard deviation scales by the magnitude of the scale")
	_, ok = Histogram{}.Mean()
	assert.False(t, ok)
}

// Histograms of the tiles of a region, merged, are the histogram of the
// region: the same counts, range and moments, for every sample kind.
func TestMergedTilesAreTheWhole(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 40).Draw(t, "w")
		h := rapid.IntRange(1, 40).Draw(t, "h")
		raw := rapid.SliceOfN(rapid.Uint16(), w*h, w*h).Draw(t, "samples")
		var p imaging.Plane
		switch kind {
		case imaging.SampleKindU8:
			s := make([]uint8, w*h)
			for i := range s {
				s[i] = uint8(raw[i])
			}
			p, _ = imaging.NewPlaneU8(w, h, s)
		case imaging.SampleKindU16:
			p, _ = imaging.NewPlaneU16(w, h, slices.Clone(raw))
		default:
			s := make([]float32, w*h)
			for i := range s {
				s[i] = float32(raw[i])/7 - 900
				if raw[i]%11 == 0 {
					s[i] = float32(math.NaN())
				}
			}
			p, _ = imaging.NewPlaneF32(w, h, s)
		}
		whole, err := OfRegion(p, image.Rect(0, 0, w, h))
		require.NoError(t, err)
		lo, hi, _ := whole.StoredRange()
		tw := rapid.IntRange(1, w).Draw(t, "tile w")
		th := rapid.IntRange(1, h).Draw(t, "tile h")
		var merged Histogram
		for y := 0; y < h; y += th {
			for x := 0; x < w; x += tw {
				part, err := OfRegionIn(p, image.Rect(x, y, min(x+tw, w), min(y+th, h)), lo, hi)
				require.NoError(t, err)
				merged, err = Merge(merged, part)
				require.NoError(t, err)
			}
		}
		require.Equal(t, whole.Valid(), merged.Valid())
		require.Equal(t, whole.Invalid(), merged.Invalid())
		if whole.Valid() == 0 {
			return
		}
		require.Equal(t, whole.Bins(), merged.Bins())
		for i := range whole.Bins() {
			require.Equal(t, whole.Count(i), merged.Count(i), "bin %d", i)
		}
		mlo, mhi, _ := merged.StoredRange()
		require.Equal(t, lo, mlo)
		require.Equal(t, hi, mhi)
		wm, _ := whole.Mean()
		mm, _ := merged.Mean()
		require.InDelta(t, wm, mm, 1e-9*(1+math.Abs(wm)))
		ws, _ := whole.StdDev()
		ms, _ := merged.StdDev()
		require.InDelta(t, ws, ms, 1e-6*(1+ws))
		for _, pct := range []float64{1, 50, 99} {
			a, _ := whole.PercentileStored(pct)
			b, _ := merged.PercentileStored(pct)
			require.Equal(t, a, b, "p=%v", pct)
		}
	})
}

func TestHistogramsThatDoNotLineUpDoNotMerge(t *testing.T) {
	a, _ := imaging.NewPlaneF32(2, 1, []float32{1, 2})
	h1, _ := OfRegionIn(a, image.Rect(0, 0, 2, 1), 0, 10)
	h2, _ := OfRegionIn(a, image.Rect(0, 0, 2, 1), 0, 20)
	_, err := Merge(h1, h2)
	assert.Error(t, err)
	b, _ := imaging.NewPlaneU8(2, 1, []uint8{1, 2})
	_, err = Merge(h1, Of(b))
	assert.Error(t, err)
	_, err = OfRegionIn(a, image.Rect(0, 0, 2, 1), 5, 1)
	assert.Error(t, err, "a descending range")
}
