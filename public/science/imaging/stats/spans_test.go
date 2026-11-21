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

// drawSpans draws spans of a w × h plane in row and column order without
// overlap: per row, nothing or up to three disjoint runs.
func drawSpans(t *rapid.T, w, h int) (spans []RowSpan) {
	for y := range h {
		x := 0
		for range rapid.IntRange(0, 3).Draw(t, "runs") {
			if x >= w {
				break
			}
			lo := rapid.IntRange(x, w-1).Draw(t, "min")
			hi := rapid.IntRange(lo+1, w).Draw(t, "max")
			spans = append(spans, RowSpan{Row: y, Min: lo, Max: hi})
			x = hi + rapid.IntRange(0, 2).Draw(t, "gap")
		}
	}
	return spans
}

// directSpans is the spans' valid stored samples, sorted, and their invalid
// count, read through the plane's own accessor.
func directSpans(p imaging.Plane, spans []RowSpan) (vals []float64, invalid int) {
	for _, s := range spans {
		for x := s.Min; x < s.Max; x++ {
			v, ok := p.StoredAt(x, s.Row)
			if !ok {
				invalid++
				continue
			}
			vals = append(vals, v)
		}
	}
	slices.Sort(vals)
	return vals, invalid
}

// For every kind, with and without a declared invalid value, a histogram over
// spans counts what a direct reading of the spans' samples says: the split
// into valid and invalid, the range, the mean and the standard deviation,
// and the nearest-rank percentiles — exactly for a kind with codes, within a
// bin otherwise.
func TestSpansAreCountedAsTheDirectReadingSays(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 20).Draw(t, "w")
		h := rapid.IntRange(1, 20).Draw(t, "h")
		p := drawPlane(t, kind, w, h)
		spans := drawSpans(t, w, h)
		got, err := OfSpans(p, spans)
		require.NoError(t, err)
		vals, invalid := directSpans(p, spans)
		require.EqualValues(t, len(vals), got.Valid())
		require.EqualValues(t, invalid, got.Invalid())
		if len(vals) == 0 {
			_, ok := got.Mean()
			require.False(t, ok)
			return
		}
		lo, hi, ok := got.StoredRange()
		require.True(t, ok)
		require.Equal(t, vals[0], lo)
		require.Equal(t, vals[len(vals)-1], hi)
		var sum float64
		for _, v := range vals {
			sum += v
		}
		mean := sum / float64(len(vals))
		var m2 float64
		for _, v := range vals {
			m2 += (v - mean) * (v - mean)
		}
		gotMean, _ := got.Mean()
		gotSd, _ := got.StdDev()
		scale := math.Max(math.Abs(vals[0]), math.Abs(vals[len(vals)-1])) + 1
		require.InDelta(t, mean, gotMean, 1e-9*scale)
		require.InDelta(t, math.Sqrt(m2/float64(len(vals))), gotSd, 1e-9*scale)
		_, _, codes := kind.Codes()
		for _, pct := range []float64{0, 1, 50, 99, 100} {
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

// The spans of every row of a rectangle are the rectangle: the same
// histogram, bin for bin.
func TestTheSpansOfARectangleCountAsTheRectangle(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		kind := rapid.SampledFrom(imaging.AllSampleKinds).Draw(t, "kind")
		w := rapid.IntRange(1, 20).Draw(t, "w")
		h := rapid.IntRange(1, 20).Draw(t, "h")
		p := drawPlane(t, kind, w, h)
		x0 := rapid.IntRange(0, w-1).Draw(t, "x0")
		x1 := rapid.IntRange(x0+1, w).Draw(t, "x1")
		y0 := rapid.IntRange(0, h-1).Draw(t, "y0")
		y1 := rapid.IntRange(y0+1, h).Draw(t, "y1")
		var spans []RowSpan
		for y := y0; y < y1; y++ {
			spans = append(spans, RowSpan{Row: y, Min: x0, Max: x1})
		}
		a, err := OfSpans(p, spans)
		require.NoError(t, err)
		b, err := OfRegion(p, image.Rect(x0, y0, x1, y1))
		require.NoError(t, err)
		require.Equal(t, b, a)
	})
}

func TestSpansOutOfOrderOverlappingOrOutsideAreRefused(t *testing.T) {
	p, err := imaging.NewPlaneU8(4, 3, make([]uint8, 12))
	require.NoError(t, err)
	for name, spans := range map[string][]RowSpan{
		"empty span":     {{Row: 0, Min: 2, Max: 2}},
		"left of plane":  {{Row: 0, Min: -1, Max: 2}},
		"right of plane": {{Row: 0, Min: 1, Max: 5}},
		"below plane":    {{Row: 3, Min: 0, Max: 1}},
		"rows backwards": {{Row: 1, Min: 0, Max: 1}, {Row: 0, Min: 0, Max: 1}},
		"overlap":        {{Row: 1, Min: 0, Max: 2}, {Row: 1, Min: 1, Max: 3}},
	} {
		_, err := OfSpans(p, spans)
		assert.Error(t, err, name)
	}
	h, err := OfSpans(p, []RowSpan{{Row: 1, Min: 0, Max: 2}, {Row: 1, Min: 2, Max: 4}})
	require.NoError(t, err)
	assert.EqualValues(t, 4, h.Valid(), "touching spans do not overlap")
	h, err = OfSpans(p, nil)
	require.NoError(t, err)
	assert.Zero(t, h.Valid())
	_, ok := h.Mean()
	assert.False(t, ok)
	merged, err := Merge(h, Of(p))
	require.NoError(t, err)
	assert.EqualValues(t, 12, merged.Valid(), "the histogram of nothing merges")
}
