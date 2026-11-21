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

// drawPlane draws a w × h plane of any of the six kinds, its samples spread
// over the kind's own range — negative ones for the signed kinds, NaN in the
// float ones — and, half the time, one stored value that occurs in it
// declared invalid (ADR-0398 §SD2).
func drawPlane(t *rapid.T, kind imaging.SampleKindE, w, h int) imaging.Plane {
	raw := rapid.SliceOfN(rapid.Uint16(), w*h, w*h).Draw(t, "samples")
	n := w * h
	var p imaging.Plane
	var err error
	switch kind {
	case imaging.SampleKindU8:
		s := make([]uint8, n)
		for i := range s {
			s[i] = uint8(raw[i])
		}
		p, err = imaging.NewPlaneU8(w, h, s)
	case imaging.SampleKindU16:
		p, err = imaging.NewPlaneU16(w, h, slices.Clone(raw))
	case imaging.SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(raw[i])
		}
		p, err = imaging.NewPlaneI16(w, h, s)
	case imaging.SampleKindI32:
		s := make([]int32, n)
		for i := range s {
			s[i] = int32(uint32(raw[i])<<16 | uint32(raw[i]*7))
		}
		p, err = imaging.NewPlaneI32(w, h, s)
	case imaging.SampleKindF32:
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(raw[i])/9 - 2000
			if raw[i]%7 == 0 {
				s[i] = float32(math.NaN())
			}
		}
		p, err = imaging.NewPlaneF32(w, h, s)
	case imaging.SampleKindF64:
		s := make([]float64, n)
		for i := range s {
			s[i] = float64(raw[i])*1e-7 - 3e-3
			if raw[i]%7 == 0 {
				s[i] = math.NaN()
			}
		}
		p, err = imaging.NewPlaneF64(w, h, s)
	default:
		t.Fatalf("no generator for kind %s", kind)
	}
	require.NoError(t, err)
	if rapid.Bool().Draw(t, "declare invalid") {
		at := rapid.IntRange(0, n-1).Draw(t, "invalid at")
		if v, ok := p.StoredAt(at%w, at/w); ok {
			p, err = p.WithInvalid(v)
			require.NoError(t, err)
		}
	}
	return p
}

// direct is the region's valid stored samples, sorted, and its invalid count,
// read from the plane's own slice: NaN and the declared value are invalid.
func direct(p imaging.Plane, rect image.Rectangle) (vals []float64, invalid int) {
	inv, hasInv := p.Invalid()
	w := p.Width()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			i := y*w + x
			var v float64
			switch p.Kind() {
			case imaging.SampleKindU8:
				v = float64(p.U8()[i])
			case imaging.SampleKindU16:
				v = float64(p.U16()[i])
			case imaging.SampleKindI16:
				v = float64(p.I16()[i])
			case imaging.SampleKindI32:
				v = float64(p.I32()[i])
			case imaging.SampleKindF32:
				v = float64(p.F32()[i])
			case imaging.SampleKindF64:
				v = p.F64()[i]
			}
			if math.IsNaN(v) || (hasInv && v == inv) {
				invalid++
				continue
			}
			vals = append(vals, v)
		}
	}
	slices.Sort(vals)
	return vals, invalid
}

// For every kind, with and without a declared invalid value: the counts
// split as the direct reading does, an exact histogram's percentiles are the
// nearest-rank sample, a binned one's are within a bin of it, and the range
// is the valid samples' range.
func TestEveryKindIsCountedAsTheDirectReadingSays(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 25).Draw(t, "w")
		h := rapid.IntRange(1, 25).Draw(t, "h")
		p := drawPlane(t, kind, w, h)
		rect := image.Rect(0, 0, w, h)
		got, err := OfRegion(p, rect)
		require.NoError(t, err)
		vals, invalid := direct(p, rect)
		require.EqualValues(t, len(vals), got.Valid())
		require.EqualValues(t, invalid, got.Invalid())
		_, _, codes := kind.Codes()
		require.Equal(t, codes, got.IsExact(), "exact follows Codes, not IsInteger")
		var total uint64
		for i := range got.Bins() {
			total += got.Count(i)
		}
		require.EqualValues(t, len(vals), total, "an invalid sample is in no bin")
		if codes {
			lo, n, _ := kind.Codes()
			require.Equal(t, n, got.Bins())
			require.Equal(t, float64(lo), got.BinStored(0))
			require.Equal(t, float64(lo+int64(n)-1), got.BinStored(n-1))
			if inv, ok := p.Invalid(); ok {
				require.Zero(t, got.Count(int(int64(inv)-lo)), "the declared code's bin is empty")
			}
		} else {
			require.Equal(t, FloatBins, got.Bins())
		}
		if len(vals) == 0 {
			_, ok := got.PercentileStored(50)
			require.False(t, ok)
			return
		}
		lo, hi, ok := got.StoredRange()
		require.True(t, ok)
		require.Equal(t, vals[0], lo)
		require.Equal(t, vals[len(vals)-1], hi)
		for _, pct := range []float64{0, 1, 13, 50, 87.5, 99, 100} {
			gotP, ok := got.PercentileStored(pct)
			require.True(t, ok)
			rank := int(math.Ceil(pct / 100 * float64(len(vals))))
			want := vals[max(rank-1, 0)]
			if codes || pct == 0 || pct == 100 {
				require.Equal(t, want, gotP, "p=%v", pct)
				continue
			}
			require.InDelta(t, want, gotP, 1.000001*got.BinWidth()+1e-12, "p=%v", pct)
		}
	})
}

func TestASignedSixteenBitHistogramIsExactBelowZero(t *testing.T) {
	p, err := imaging.NewPlaneI16(6, 1, []int16{-32768, -5, -5, 0, 7, 32767})
	require.NoError(t, err)
	h := Of(p)
	assert.True(t, h.IsExact())
	assert.Equal(t, 65536, h.Bins())
	assert.Equal(t, -32768.0, h.BinStored(0))
	assert.Equal(t, -5.0, h.BinStored(32763))
	assert.EqualValues(t, 2, h.Count(32763))
	assert.Equal(t, 1.0, h.BinWidth())
	lo, hi, _ := h.StoredRange()
	assert.Equal(t, -32768.0, lo)
	assert.Equal(t, 32767.0, hi)
	med, _ := h.PercentileStored(50)
	assert.Equal(t, -5.0, med, "the lower median of −32768, −5, −5, 0, 7, 32767")

	q, err := p.WithInvalid(-5)
	require.NoError(t, err)
	hq := Of(q)
	assert.EqualValues(t, 4, hq.Valid())
	assert.EqualValues(t, 2, hq.Invalid())
	assert.Zero(t, hq.Count(32763))
	mean, _ := hq.Mean()
	assert.Equal(t, (-32768.0+0+7+32767)/4, mean)
}

func TestThirtyTwoBitIntegersAndDoublesAreBinned(t *testing.T) {
	p, err := imaging.NewPlaneI32(4, 1, []int32{math.MinInt32, -1, 1, math.MaxInt32})
	require.NoError(t, err)
	h := Of(p)
	assert.False(t, h.IsExact(), "an integer kind with too many codes is binned")
	assert.Equal(t, FloatBins, h.Bins())
	lo, hi, _ := h.StoredRange()
	assert.Equal(t, float64(math.MinInt32), lo)
	assert.Equal(t, float64(math.MaxInt32), hi)
	assert.InDelta(t, (float64(math.MaxInt32)-float64(math.MinInt32))/FloatBins, h.BinWidth(), 1e-6)
	mean, _ := h.Mean()
	assert.Equal(t, -0.25, mean, "the sum cannot wrap: it is taken in float64")

	q, err := p.WithInvalid(math.MaxInt32)
	require.NoError(t, err)
	hq := Of(q)
	assert.EqualValues(t, 1, hq.Invalid())
	_, hi, _ = hq.StoredRange()
	assert.Equal(t, 1.0, hi)

	f, err := imaging.NewPlaneF64(3, 1, []float64{1e-300, math.NaN(), -9999})
	require.NoError(t, err)
	f, err = f.WithInvalid(-9999)
	require.NoError(t, err)
	hf := Of(f)
	assert.EqualValues(t, 1, hf.Valid())
	assert.EqualValues(t, 2, hf.Invalid(), "NaN and the declared value")
	m, _ := hf.Mean()
	assert.Equal(t, 1e-300, m)
}

func TestAKindThisPackageDoesNotCountIsRefusedByName(t *testing.T) {
	_, err := OfRegion(imaging.Plane{}, image.Rect(0, 0, 1, 1))
	assert.Error(t, err)
	assert.Zero(t, Of(imaging.Plane{}).Valid())
}

// A region of a 32-bit or 64-bit plane counted over a given bin range lines
// up with another's, so the two merge into the histogram of both.
func TestBinnedKindsMergeOverAGivenRange(t *testing.T) {
	for _, kind := range []imaging.SampleKindE{imaging.SampleKindI32, imaging.SampleKindF64} {
		var p imaging.Plane
		var err error
		if kind == imaging.SampleKindI32 {
			p, err = imaging.NewPlaneI32(4, 1, []int32{-100, 5, 9, 1000})
		} else {
			p, err = imaging.NewPlaneF64(4, 1, []float64{-100, 5, 9, 1000})
		}
		require.NoError(t, err)
		p, err = p.WithInvalid(9)
		require.NoError(t, err)
		a, err := OfRegionIn(p, image.Rect(0, 0, 2, 1), -100, 1000)
		require.NoError(t, err)
		b, err := OfRegionIn(p, image.Rect(2, 0, 4, 1), -100, 1000)
		require.NoError(t, err)
		m, err := Merge(a, b)
		require.NoError(t, err)
		assert.EqualValues(t, 3, m.Valid(), kind.String())
		assert.EqualValues(t, 1, m.Invalid())
		mean, _ := m.Mean()
		assert.InDelta(t, 905.0/3, mean, 1e-9)
	}
}
