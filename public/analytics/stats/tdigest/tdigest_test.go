package tdigest

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rankError computes |actualRank(estimate)/n - q| for a sorted dataset.
// This is the canonical metric for quantile-sketch quality: how far off
// is the estimate's true position in the dataset from the requested q?
func rankError(estimate float64, q float64, sorted []float64) float64 {
	actualRank := 0
	for _, v := range sorted {
		if v <= estimate {
			actualRank++
		} else {
			break
		}
	}
	actualQ := float64(actualRank) / float64(len(sorted))
	return math.Abs(actualQ - q)
}

func gaussianData(rnd *rand.Rand, n int) []float64 {
	data := make([]float64, n)
	for i := range data {
		data[i] = rnd.NormFloat64()
	}
	return data
}

func cauchyData(rnd *rand.Rand, n int) []float64 {
	data := make([]float64, n)
	for i := range data {
		u := rnd.Float64()
		if u < 1e-9 {
			u = 1e-9
		} else if u > 1-1e-9 {
			u = 1 - 1e-9
		}
		data[i] = math.Tan(math.Pi * (u - 0.5))
	}
	return data
}

func lognormalData(rnd *rand.Rand, n int) []float64 {
	data := make([]float64, n)
	for i := range data {
		data[i] = math.Exp(rnd.NormFloat64())
	}
	return data
}

func exponentialData(rnd *rand.Rand, n int) []float64 {
	data := make([]float64, n)
	for i := range data {
		data[i] = rnd.ExpFloat64()
	}
	return data
}

func TestEmpty(t *testing.T) {
	d := NewTDigest()
	require.True(t, math.IsNaN(d.Quantile(0.5)))
	require.True(t, math.IsNaN(d.CDF(0)))
	require.True(t, math.IsNaN(d.Min()))
	require.True(t, math.IsNaN(d.Max()))
	require.Equal(t, int64(0), d.Count())
	require.Equal(t, float64(0), d.Weight())
}

func TestSingleObservation(t *testing.T) {
	d := NewTDigest()
	d.Push(42.0)
	require.Equal(t, int64(1), d.Count())
	require.InDelta(t, 42.0, d.Min(), 1e-12)
	require.InDelta(t, 42.0, d.Max(), 1e-12)
	require.InDelta(t, 42.0, d.Quantile(0.0), 1e-12)
	require.InDelta(t, 42.0, d.Quantile(1.0), 1e-12)
	require.InDelta(t, 42.0, d.Quantile(0.5), 1e-12)
	require.InDelta(t, 1.0, d.CDF(43.0), 1e-12)
	require.InDelta(t, 0.0, d.CDF(41.0), 1e-12)
}

func TestIdenticalValues(t *testing.T) {
	d := NewTDigest()
	for range 1_000 {
		d.Push(7.5)
	}
	require.InDelta(t, 7.5, d.Quantile(0.0), 1e-12)
	require.InDelta(t, 7.5, d.Quantile(0.5), 1e-12)
	require.InDelta(t, 7.5, d.Quantile(1.0), 1e-12)
	require.InDelta(t, 7.5, d.Min(), 1e-12)
	require.InDelta(t, 7.5, d.Max(), 1e-12)
}

func TestNaNAndInfDropped(t *testing.T) {
	d := NewTDigest()
	d.Push(math.NaN())
	d.Push(math.Inf(1))
	d.Push(math.Inf(-1))
	d.PushWeighted(1.0, math.NaN())
	d.PushWeighted(1.0, math.Inf(1))
	d.PushWeighted(1.0, -1.0)
	d.PushWeighted(1.0, 0.0)
	require.Equal(t, int64(0), d.Count())
}

func TestMonotonicAscending(t *testing.T) {
	d := NewTDigest()
	for i := range 10_000 {
		d.Push(float64(i))
	}
	require.InDelta(t, 0.0, d.Min(), 1e-12)
	require.InDelta(t, 9999.0, d.Max(), 1e-12)

	prev := d.Quantile(0.0)
	for q := 0.01; q <= 1.0; q += 0.01 {
		v := d.Quantile(q)
		require.LessOrEqual(t, prev, v+1e-9, "quantile not monotone at q=%v", q)
		prev = v
	}
}

func TestAccuracyAcrossDistributions(t *testing.T) {
	type accCase struct {
		name string
		gen  func(*rand.Rand, int) []float64
		// per-quantile tolerance on rank error.
		tail float64
		body float64
	}
	cases := []accCase{
		{"Gaussian", gaussianData, 0.005, 0.015},
		{"Lognormal", lognormalData, 0.005, 0.015},
		{"Exponential", exponentialData, 0.005, 0.015},
		{"Cauchy", cauchyData, 0.010, 0.020},
	}
	qs := []float64{0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 0.75, 0.9, 0.95, 0.99, 0.999}

	const n = 100_000
	for ci, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rnd := rand.New(rand.NewSource(int64(ci)*1000 + 1))
			data := c.gen(rnd, n)
			sorted := slices.Clone(data)
			slices.Sort(sorted)

			d := NewTDigest()
			for _, v := range data {
				d.Push(v)
			}
			require.Equal(t, int64(n), d.Count())

			for _, q := range qs {
				est := d.Quantile(q)
				e := rankError(est, q, sorted)
				tol := c.body
				if q <= 0.05 || q >= 0.95 {
					tol = c.tail
				}
				assert.LessOrEqual(t, e, tol,
					"%s q=%v: rank err %.4f > tol %.4f (est=%v)", c.name, q, e, tol, est)
			}
		})
	}
}

func TestQuantileCDFInverse(t *testing.T) {
	rnd := rand.New(rand.NewSource(99))
	d := NewTDigest()
	const n = 50_000
	for range n {
		d.Push(rnd.NormFloat64())
	}

	for _, q := range []float64{0.05, 0.1, 0.25, 0.5, 0.75, 0.9, 0.95} {
		x := d.Quantile(q)
		qBack := d.CDF(x)
		assert.InDelta(t, q, qBack, 0.01, "inverse failed at q=%v: Quantile=%v, CDF=%v", q, x, qBack)
	}
}

func TestCDFBoundsAndMonotone(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	d := NewTDigest()
	for range 10_000 {
		d.Push(rnd.NormFloat64())
	}
	require.InDelta(t, 0.0, d.CDF(d.Min()-1e9), 1e-12)
	require.InDelta(t, 1.0, d.CDF(d.Max()+1e9), 1e-12)

	prev := 0.0
	for x := -5.0; x <= 5.0; x += 0.1 {
		c := d.CDF(x)
		require.LessOrEqual(t, prev, c+1e-9, "CDF not monotone at x=%v", x)
		require.GreaterOrEqual(t, c, 0.0-1e-12)
		require.LessOrEqual(t, c, 1.0+1e-12)
		prev = c
	}
}

func TestQuantilesBatch(t *testing.T) {
	rnd := rand.New(rand.NewSource(13))
	d := NewTDigest()
	for range 10_000 {
		d.Push(rnd.NormFloat64())
	}
	qs := []float64{0.01, 0.5, 0.99}
	out := d.Quantiles(qs)
	require.Len(t, out, 3)
	for i, q := range qs {
		require.InDelta(t, d.Quantile(q), out[i], 1e-12)
	}
}

func TestMergeAssociativity(t *testing.T) {
	rnd := rand.New(rand.NewSource(123))
	const n = 100_000
	data := gaussianData(rnd, n)

	full := NewTDigest()
	for _, v := range data {
		full.Push(v)
	}

	parts := make([]*TDigest, 4)
	for i := range parts {
		parts[i] = NewTDigest()
	}
	for i, v := range data {
		parts[i%4].Push(v)
	}
	merged := NewTDigest()
	for _, p := range parts {
		merged.Merge(p)
	}

	require.Equal(t, full.Count(), merged.Count())
	require.InDelta(t, full.Min(), merged.Min(), 1e-12)
	require.InDelta(t, full.Max(), merged.Max(), 1e-12)
	require.InDelta(t, full.Weight(), merged.Weight(), 1e-9)

	sorted := slices.Clone(data)
	slices.Sort(sorted)
	for _, q := range []float64{0.01, 0.05, 0.5, 0.95, 0.99} {
		em := rankError(merged.Quantile(q), q, sorted)
		assert.LessOrEqual(t, em, 0.025, "merged rank err at q=%v: %v", q, em)
	}
}

func TestMergeEmptyOther(t *testing.T) {
	a := NewTDigest()
	for range 1_000 {
		a.Push(1.0)
	}
	b := NewTDigest()
	cnt := a.Count()
	a.Merge(b)
	require.Equal(t, cnt, a.Count())
}

func TestMergeIntoEmpty(t *testing.T) {
	a := NewTDigest()
	b := NewTDigest()
	rnd := rand.New(rand.NewSource(5))
	for range 1_000 {
		b.Push(rnd.NormFloat64())
	}
	a.Merge(b)
	require.Equal(t, b.Count(), a.Count())
	require.InDelta(t, b.Min(), a.Min(), 1e-12)
	require.InDelta(t, b.Max(), a.Max(), 1e-12)
}

func TestPushWeighted(t *testing.T) {
	d := NewTDigest()
	d.PushWeighted(10.0, 5.0)
	d.PushWeighted(20.0, 5.0)
	require.Equal(t, int64(2), d.Count())
	require.InDelta(t, 10.0, d.Min(), 1e-12)
	require.InDelta(t, 20.0, d.Max(), 1e-12)
	require.InDelta(t, 10.0, d.Weight(), 1e-12)
	require.InDelta(t, 15.0, d.Quantile(0.5), 1e-9)
}

func TestCentroidCount(t *testing.T) {
	rnd := rand.New(rand.NewSource(17))
	d := NewTDigestWithDelta(100)
	for range 100_000 {
		d.Push(rnd.NormFloat64())
	}
	c := d.CentroidCount()
	require.Greater(t, c, 20, "too few centroids (%d)", c)
	require.Less(t, c, 100, "too many centroids (%d) for δ=100", c)
}

func TestJSONRoundTrip(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	d := NewTDigest()
	for range 10_000 {
		d.Push(rnd.NormFloat64())
	}

	data, err := json.Marshal(d)
	require.NoError(t, err)

	d2 := NewTDigest()
	err = json.Unmarshal(data, d2)
	require.NoError(t, err)

	require.Equal(t, d.Count(), d2.Count())
	require.InDelta(t, d.Min(), d2.Min(), 1e-12)
	require.InDelta(t, d.Max(), d2.Max(), 1e-12)
	require.InDelta(t, d.Weight(), d2.Weight(), 1e-12)
	for _, q := range []float64{0.01, 0.05, 0.25, 0.5, 0.75, 0.95, 0.99} {
		require.InDelta(t, d.Quantile(q), d2.Quantile(q), 1e-9)
	}

	// Continue pushing into the restored digest.
	d2.Push(99.0)
	require.Equal(t, d.Count()+1, d2.Count())
}

func TestJSONRoundTripEmpty(t *testing.T) {
	d := NewTDigest()
	data, err := json.Marshal(d)
	require.NoError(t, err)
	d2 := NewTDigest()
	require.NoError(t, json.Unmarshal(data, d2))
	require.Equal(t, int64(0), d2.Count())
	require.True(t, math.IsInf(d2.min, 1))
	require.True(t, math.IsInf(d2.max, -1))
}

func TestJSONInvalidLengthMismatch(t *testing.T) {
	bad := `{"delta":100,"n":2,"total_weight":2,"min":0,"max":1,"means":[0,1],"weights":[1]}`
	d := NewTDigest()
	err := json.Unmarshal([]byte(bad), d)
	require.Error(t, err)
}

func TestCentroidsIterator(t *testing.T) {
	rnd := rand.New(rand.NewSource(19))
	d := NewTDigest()
	for range 5_000 {
		d.Push(rnd.NormFloat64())
	}
	var (
		total float64
		prev  = math.Inf(-1)
		count int
	)
	for _, c := range d.Centroids() {
		require.LessOrEqual(t, prev, c.Mean, "centroids not sorted")
		require.Greater(t, c.Weight, 0.0)
		total += c.Weight
		prev = c.Mean
		count++
	}
	require.Equal(t, d.CentroidCount(), count)
	require.InDelta(t, d.Weight(), total, 1e-9)
}

func TestDeltaClamping(t *testing.T) {
	require.InDelta(t, minDelta, NewTDigestWithDelta(1).Delta(), 1e-12)
	require.InDelta(t, maxDelta, NewTDigestWithDelta(1e9).Delta(), 1e-12)
	require.InDelta(t, defaultDelta, NewTDigest().Delta(), 1e-12)
	require.InDelta(t, minDelta, NewTDigestWithDelta(math.NaN()).Delta(), 1e-12)
}

func BenchmarkPush(b *testing.B) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	data := make([]float64, b.N)
	for i := range data {
		data[i] = rnd.NormFloat64()
	}
	d := NewTDigest()
	b.ResetTimer()
	for i := range b.N {
		d.Push(data[i])
	}
}

func BenchmarkQuantile(b *testing.B) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	d := NewTDigest()
	for range 1_000_000 {
		d.Push(rnd.NormFloat64())
	}
	b.ResetTimer()
	for i := range b.N {
		q := float64(i%99+1) / 100.0
		_ = d.Quantile(q)
	}
}

// FuzzTDigest verifies the digest cannot panic and basic monotonicity
// invariants hold over arbitrary finite inputs.
func FuzzTDigest(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw)%8 != 0 || len(raw) < 16 {
			return
		}
		floats := make([]float64, len(raw)/8)
		for i := range floats {
			bits := binary.LittleEndian.Uint64(raw[i*8:])
			v := math.Float64frombits(bits)
			if math.IsNaN(v) || math.IsInf(v, 0) {
				v = 0
			}
			floats[i] = v
		}
		d := NewTDigest()
		for _, v := range floats {
			d.Push(v)
		}
		if d.Count() == 0 {
			return
		}
		q01 := d.Quantile(0.01)
		q50 := d.Quantile(0.5)
		q99 := d.Quantile(0.99)
		if math.IsNaN(q01) || math.IsNaN(q50) || math.IsNaN(q99) {
			t.Errorf("unexpected NaN: %v %v %v", q01, q50, q99)
		}
		// Use a magnitude-aware tolerance: float64 has ~16 digits of
		// precision, so at |v| ~ 1e+281 the linear-interpolation result
		// loses absolute precision proportional to the magnitude.
		relSlack := func(a, b float64) float64 {
			m := math.Max(math.Abs(a), math.Abs(b))
			if m < 1.0 {
				return 1e-9
			}
			return m * 1e-12
		}
		if q01-q50 > relSlack(q01, q50) {
			t.Errorf("monotonicity broken: q01=%v > q50=%v", q01, q50)
		}
		if q50-q99 > relSlack(q50, q99) {
			t.Errorf("monotonicity broken: q50=%v > q99=%v", q50, q99)
		}
		if q01 < d.Min()-relSlack(q01, d.Min()) {
			t.Errorf("q01=%v < min=%v", q01, d.Min())
		}
		if q99 > d.Max()+relSlack(q99, d.Max()) {
			t.Errorf("q99=%v > max=%v", q99, d.Max())
		}
	})
}
