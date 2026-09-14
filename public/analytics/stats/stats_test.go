package stats

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"math/big"
	"math/rand"
	"slices"
	"testing"
	"time"
)

// --- Helper: Arbitrary Precision "Source of Truth" ---

// exactPrec is the math/big precision exactStats computes at. big.NewFloat
// defaults to 53 bits — float64 arithmetic with correct rounding, not a
// reference. A float64 spans 2^-1074..2^1024, so a sum needs about 2100 bits
// to stay exact and a squared difference about twice that; the quotients by n
// round, far below any tolerance the tests apply.
const exactPrec = 4400

func exactFloat(v float64) *big.Float {
	return new(big.Float).SetPrec(exactPrec).SetFloat64(v)
}

// exactStats calculates mean and variance using math/big at exactPrec.
func exactStats(data []float64) (mean, variance float64) {
	if len(data) == 0 {
		return 0, 0
	}

	n := exactFloat(float64(len(data)))
	sum := exactFloat(0)

	// 1. Calculate Exact Mean
	for _, v := range data {
		sum.Add(sum, exactFloat(v))
	}

	bigMean := exactFloat(0).Quo(sum, n)

	// 2. Calculate Exact Variance
	// sum((x - mean)^2)
	sumSqDiff := exactFloat(0)
	for _, v := range data {
		diff := exactFloat(0).Sub(exactFloat(v), bigMean)
		sq := exactFloat(0).Mul(diff, diff)
		sumSqDiff.Add(sumSqDiff, sq)
	}

	var bigVar *big.Float
	if len(data) > 1 {
		bigVar = exactFloat(0).Quo(sumSqDiff, exactFloat(float64(len(data)-1)))
	} else {
		bigVar = exactFloat(0)
	}

	mean, _ = bigMean.Float64()
	variance, _ = bigVar.Float64()
	return
}

// assertClose checks if two float64s are within a small epsilon.
// We use a relative error check for large numbers and absolute for small ones.
func assertClose(t *testing.T, name string, expected, actual float64) {
	assertCloseTol(t, name, expected, actual, 1e-12) // High precision requirement
}

func assertCloseTol(t *testing.T, name string, expected, actual, epsilon float64) {
	t.Helper()
	diff := math.Abs(expected - actual)

	// Handle zero cases strictly
	if expected == 0 {
		if diff > epsilon {
			t.Errorf("[%s] Expected 0, got %.15f (diff: %.15f)", name, actual, diff)
		}
		return
	}

	// Relative error calculation
	relError := diff / math.Abs(expected)
	if relError > epsilon {
		t.Errorf("[%s] Not close enough.\nExpected: %.15f\nActual:   %.15f\nRelErr:   %.15e",
			name, expected, actual, relError)
	}
}

// --- Tests ---

func TestEdgeCases(t *testing.T) {
	s := NewStreamStats()

	// Case 0: Empty
	if s.Count() != 0 || s.Mean() != 0 || s.Variance() != 0 {
		t.Error("Empty stats should be zeroed")
	}

	// Case 1: One Item
	s.Push(5.0)
	if s.Count() != 1 || s.Mean() != 5.0 || s.Variance() != 0 {
		t.Error("Single item stats failed")
	}

	// Case 2: Identical Items (Variance should be 0)
	s.Push(5.0)
	s.Push(5.0)
	if s.Variance() != 0 {
		t.Errorf("Identical items should have 0 variance, got %v", s.Variance())
	}
}

func Test_vsArbitraryPrecision(t *testing.T) {
	rnd := rand.New(rand.NewSource(42)) // Deterministic random

	scenarios := []struct {
		name string
		data []float64
		// epsilon overrides assertClose's relative tolerance when non-zero.
		epsilon float64
	}{
		{
			name: "Small Integers",
			data: []float64{1, 2, 3, 4, 5},
		},
		{
			name: "Large Offset",
			// Data: 100,000,001 to 100,000,005.
			// Naive SumSq algorithms often fail here.
			data: []float64{1e8 + 1, 1e8 + 2, 1e8 + 3, 1e8 + 4, 1e8 + 5},
		},
		{
			name: "Mixed Magnitudes",
			// Mixing large and very small numbers tests the Kahan summation.
			// The 1e-5 terms meet a running mean near 1e9, whose ulp is
			// 1.2e-7, so x - mean rounds them away before compensation can
			// keep them: the stream lands ~2.7e-11 (relative) off the exact
			// mean. That is float64's floor for a streaming update, not a
			// defect.
			data:    []float64{1e9, 1e-5, -1e9, -1e-5, 500, 0.0001},
			epsilon: 1e-10,
		},
		{
			name: "Random Uniform (Size 1000)",
			data: func() []float64 {
				d := make([]float64, 1000)
				for i := range d {
					d[i] = rnd.Float64() * 1000
				}
				return d
			}(),
		},
		{
			name: "Random Normal (Size 1000)",
			data: func() []float64 {
				d := make([]float64, 1000)
				for i := range d {
					d[i] = rnd.NormFloat64() * 50
				}
				return d
			}(),
		},
	}

	stats := NewStreamStats()
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			stats.Reset()
			for _, v := range sc.data {
				stats.Push(v)
			}

			expectedMean, expectedVar := exactStats(sc.data)
			expectedStdDev := math.Sqrt(expectedVar)

			epsilon := sc.epsilon
			if epsilon == 0 {
				epsilon = 1e-12
			}
			assertCloseTol(t, "Mean", expectedMean, stats.Mean(), epsilon)
			assertCloseTol(t, "Variance", expectedVar, stats.Variance(), epsilon)
			assertCloseTol(t, "StdDev", expectedStdDev, stats.StdDev(), epsilon)
		})
	}
}

// TestKahanValidation attempts to demonstrate why Kahan is needed.
// We construct a scenario where naive floating point addition drift is apparent.
func TestKahanValidation(t *testing.T) {
	stats := NewStreamStats()

	// Start with a large base
	base := 1e9
	stats.Push(base)

	// Add many small increments.
	// In standard float64: 1e9 + 1e-7 == 1e9 (the small number vanishes).
	// We use a slightly larger increment to accumulate error over many iterations.
	count := 1_000_000
	inc := 1.0

	for range count {
		stats.Push(base + inc)
	}

	// Generate the exact data slice to compare
	data := make([]float64, 0, count+1)
	data = append(data, base)
	for range count {
		data = append(data, base+inc)
	}

	expMean, expVar := exactStats(data)

	// We use a tighter check here to prove high precision
	if math.Abs(stats.Mean()-expMean) > 1e-8 {
		t.Errorf("Mean drifted significantly. Kahan might not be working.\nGot: %.10f\nExp: %.10f", stats.Mean(), expMean)
	}

	// Variance is usually where Naive implementations explode with large offsets.
	// Since all values are close to 1e9, (x - mean)^2 is small, but cancellation is huge.
	if math.Abs(stats.Variance()-expVar) > 1e-8 {
		t.Errorf("Variance drifted.\nGot: %.10f\nExp: %.10f", stats.Variance(), expVar)
	}
}

// Benchmark to check if the Kahan overhead is acceptable
func BenchmarkStreamStats(b *testing.B) {
	s := NewStreamStats()
	data := rand.New(rand.NewSource(time.Now().UnixNano()))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Push(data.Float64())
	}
}

func TestJSONSerialization(t *testing.T) {
	s := NewStreamStats()
	s.Push(10)
	s.Push(20)

	bytes, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	s2 := NewStreamStats()
	if err := json.Unmarshal(bytes, s2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if s.Mean() != s2.Mean() || s.Variance() != s2.Variance() {
		t.Error("Restored state does not match original")
	}

	// Continue using restored state
	s2.Push(30) // 10, 20, 30 -> Mean 20
	if s2.Mean() != 20.0 {
		t.Errorf("Restored object failed to calculate correct mean. Got %f", s2.Mean())
	}
}

// FuzzStreamStats reads the input as little-endian float64s and compares
// the streaming mean and variance against exactStats.
//
// A relative tolerance alone is not a sound oracle: Welford's update rounds
// every x - mean at the magnitude of the larger operand, so data that cancels
// (1e17, 1, -1e17) leaves an absolute error of order eps·max|x| that can
// dwarf a small exact result. The tolerance is therefore the larger of a
// relative one and that forward-error bound, scaled by n for accumulation and
// by max|x| again for the variance's squares, and never below n subnormal
// steps. Inputs are held below a quarter
// of the magnitude at which the intermediate differences (for the mean) or
// their squares (for the variance) overflow; past it the stream reports ±Inf
// for a finite result, which is float64 range rather than a defect.
func FuzzStreamStats(f *testing.F) {
	f.Add(float64Bytes(0, 0))
	f.Add(float64Bytes(1, 2, 3, 4))
	f.Add(float64Bytes(-1.5, 1e-300, 2.5e10, 7))
	f.Add(float64Bytes(1e17, 1, -1e17))
	f.Add(float64Bytes(1e150, -1e150, 3))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Read whole float64s and drop a partial trailing one, so every
		// mutation of the length still reaches the code under test.
		n := len(data) / 8
		if n < 2 {
			return
		}
		floats := make([]float64, n)
		maxAbs := 0.0
		for i := range floats {
			v := math.Float64frombits(binary.LittleEndian.Uint64(data[i*8:]))
			if math.IsInf(v, 0) || math.IsNaN(v) {
				return // non-finite input propagates by design
			}
			floats[i] = v
			maxAbs = math.Max(maxAbs, math.Abs(v))
		}

		stats := NewStreamStats()
		stats.PushSeq(slices.Values(floats))
		expMean, expVar := exactStats(floats)

		const relTol = 1e-9
		const slack = 16.0
		eps := math.Nextafter(1, 2) - 1
		// Rounding cannot be finer than one subnormal step, so the bound
		// never falls below n of them however small the inputs are.
		floor := slack * float64(n) * math.SmallestNonzeroFloat64
		within := func(exp, got, bound float64) bool {
			return math.Abs(exp-got) <= math.Max(relTol*math.Abs(exp), bound+floor)
		}

		if maxAbs <= math.MaxFloat64/4 {
			bound := slack * float64(n) * eps * maxAbs
			if !within(expMean, stats.Mean(), bound) {
				t.Errorf("mean: stream=%.17g exact=%.17g bound=%.3g n=%d", stats.Mean(), expMean, bound, n)
			}
		}
		if maxAbs <= math.Sqrt(math.MaxFloat64)/4 {
			bound := slack * float64(n) * eps * maxAbs * maxAbs
			if !within(expVar, stats.Variance(), bound) {
				t.Errorf("variance: stream=%.17g exact=%.17g bound=%.3g n=%d", stats.Variance(), expVar, bound, n)
			}
		}
	})
}

// refStats calculates stats using a 2-pass approach (less prone to error than 1-pass naive)
// This serves as the "Ground Truth" for Skewness and Kurtosis.
func refStats(data []float64) (mean, variance, skew, kurt, minVal, maxVal float64) {
	if len(data) == 0 {
		return
	}
	n := float64(len(data))
	minVal = data[0]
	maxVal = data[0]

	// Pass 1: Mean & Min/Max
	sum := 0.0
	for _, v := range data {
		sum += v
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	mean = sum / n

	if len(data) < 2 {
		return
	}

	// Pass 2: Moments
	var m2, m3, m4 float64
	for _, v := range data {
		delta := v - mean
		m2 += delta * delta
		m3 += delta * delta * delta
		m4 += delta * delta * delta * delta
	}

	variance = m2 / (n - 1)

	if m2 == 0 {
		return
	}

	// Skewness
	if len(data) >= 3 {
		skew = math.Sqrt(n) * m3 / math.Pow(m2, 1.5)
	}

	// Kurtosis
	if len(data) >= 4 {
		kurt = (n*m4)/(m2*m2) - 3.0
	}
	return
}

func TestExtendedStats(t *testing.T) {
	rnd := rand.New(rand.NewSource(99))

	scenarios := []struct {
		name string
		data []float64
		// epsilon overrides assertClose's relative tolerance when non-zero.
		epsilon float64
	}{
		{
			name: "Sequence 1-10",
			data: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name: "Symmetric Distribution (Zero Skew)",
			data: []float64{10, 20, 30, 20, 10},
		},
		{
			name: "Right Skewed",
			data: []float64{1, 1, 1, 1, 10},
		},
		{
			name: "Flat (Zero Variance)",
			data: []float64{5, 5, 5, 5, 5},
		},
		{
			name: "Random Normal",
			data: func() []float64 {
				d := make([]float64, 1000)
				for i := range d {
					d[i] = rnd.NormFloat64()
				}
				return d
			}(),
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			s := NewStreamStats()
			for _, v := range sc.data {
				s.Push(v)
			}

			expMean, expVar, expSkew, expKurt, expMin, expMax := refStats(sc.data)

			assertClose(t, "Mean", expMean, s.Mean())
			assertClose(t, "Variance", expVar, s.Variance())
			assertClose(t, "Min", expMin, s.Min())
			assertClose(t, "Max", expMax, s.Max())

			// Handle cases where Skew/Kurt are 0 (e.g. constant data)
			if s.Variance() > 1e-9 {
				assertClose(t, "Skewness", expSkew, s.Skewness())
				assertClose(t, "Kurtosis", expKurt, s.Kurtosis())
			}
		})
	}
}

func TestMinMaxInitialization(t *testing.T) {
	s := NewStreamStats()

	// Test Min/Max on first element
	s.Push(10.0)
	if s.Min() != 10.0 || s.Max() != 10.0 {
		t.Error("First element should set both Min and Max")
	}

	s.Push(5.0) // New Min
	if s.Min() != 5.0 || s.Max() != 10.0 {
		t.Error("Min failed to update")
	}

	s.Push(20.0) // New Max
	if s.Min() != 5.0 || s.Max() != 20.0 {
		t.Error("Max failed to update")
	}
}

func TestSkewKurtosisEdgeCases(t *testing.T) {
	s := NewStreamStats()

	// Count < 3
	s.Push(1)
	s.Push(2)
	if s.Skewness() != 0 {
		t.Error("Skewness should be 0 for N < 3")
	}

	// Count < 4
	s.Push(3)
	if s.Kurtosis() != 0 {
		t.Error("Kurtosis should be 0 for N < 4")
	}

	// Valid now
	s.Push(4)
	if s.Variance() <= 0 {
		t.Fatal("Variance should be positive for 1,2,3,4")
	}
	if s.Kurtosis() == 0 {
		t.Error("Kurtosis should be computed for N >= 4")
	}
}
func TestMergeHigherMoments(t *testing.T) {
	// Generate a dataset with distinct features (skewed, etc.)
	// We use 0..99.
	data := make([]float64, 100)
	for i := range 100 {
		data[i] = float64(i)
	}

	// 1. Calculate Reference (Sequential)
	ref := NewStreamStats()
	for _, v := range data {
		ref.Push(v)
	}

	// 2. Calculate Parallel (Split and Merge)
	partA := NewStreamStats()
	partB := NewStreamStats()

	// Split data in half (0..49 to A, 50..99 to B)
	mid := len(data) / 2
	for _, v := range data[:mid] {
		partA.Push(v)
	}
	for _, v := range data[mid:] {
		partB.Push(v)
	}

	// Merge B into A
	partA.Merge(partB)

	// 3. Compare Results
	// We expect extremely high precision equality here.
	assertClose(t, "Count", float64(ref.Count()), float64(partA.Count()))
	assertClose(t, "Min", ref.Min(), partA.Min())
	assertClose(t, "Max", ref.Max(), partA.Max())
	assertClose(t, "Mean", ref.Mean(), partA.Mean())
	assertClose(t, "Variance", ref.Variance(), partA.Variance())
	assertClose(t, "Skewness", ref.Skewness(), partA.Skewness())
	assertClose(t, "Kurtosis", ref.Kurtosis(), partA.Kurtosis())
}

// float64Bytes encodes values little-endian, the layout FuzzStreamStats reads.
func float64Bytes(values ...float64) []byte {
	out := make([]byte, 0, 8*len(values))
	for _, v := range values {
		out = binary.LittleEndian.AppendUint64(out, math.Float64bits(v))
	}
	return out
}
