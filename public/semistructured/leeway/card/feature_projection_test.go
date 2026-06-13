package card

import (
	"math"
	"testing"

	"gonum.org/v1/gonum/mat"
)

// helper to build an nRows×NumFeatures Dense with all zeros, then apply overrides.
func makeFeatureMatrix(nRows int, overrides map[[2]int]float64) *mat.Dense {
	m := mat.NewDense(nRows, NumFeatures, nil)
	data := m.RawMatrix().Data
	stride := m.RawMatrix().Stride
	for pos, v := range overrides {
		data[pos[0]*stride+pos[1]] = v
	}
	return m
}

// ─── log1p + z-score on a log-transformed feature ───────────────────────────

func TestPreprocess_LogTransformAndStandardise(t *testing.T) {
	nRows := 3
	// Feature 0 (TotalAttributeCount) is log-transformed.
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 0}: 1.0,
		{1, 0}: 100.0,
		{2, 0}: 10000.0,
	})

	if err := PreprocessFeatureMatrix(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After log1p + z-score the column mean must be ≈0.
	data := m.RawMatrix().Data
	stride := m.RawMatrix().Stride
	sum := 0.0
	for ri := 0; ri < nRows; ri++ {
		sum += data[ri*stride+0]
	}
	if math.Abs(sum/float64(nRows)) > 1e-10 {
		t.Errorf("expected mean ≈ 0 after standardisation, got %v", sum/float64(nRows))
	}
}

// ─── Bounded feature (no log) standardisation ───────────────────────────────

func TestPreprocess_BoundedFeatureNoLog(t *testing.T) {
	nRows := 2
	// Feature 6 (UntaggedAttrFraction): bounded [0,1], not log-transformed.
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 6}: 0.2,
		{1, 6}: 0.8,
	})

	if err := PreprocessFeatureMatrix(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mean := (0.2 + 0.8) / 2
	variance := (math.Pow(0.2-mean, 2) + math.Pow(0.8-mean, 2)) / 2
	std := math.Sqrt(variance)

	data := m.RawMatrix().Data
	stride := m.RawMatrix().Stride

	expected0 := (0.2 - mean) / std
	expected1 := (0.8 - mean) / std

	if math.Abs(data[0*stride+6]-expected0) > 1e-10 {
		t.Errorf("row 0: expected %v, got %v", expected0, data[0*stride+6])
	}
	if math.Abs(data[1*stride+6]-expected1) > 1e-10 {
		t.Errorf("row 1: expected %v, got %v", expected1, data[1*stride+6])
	}
}

// ─── Zero-variance (constant column) → zeroed out ──────────────────────────

func TestPreprocess_ZeroVariance(t *testing.T) {
	nRows := 4
	// Feature 2 (GiniAttrsPerSection): all rows set to 0.5.
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 2}: 0.5,
		{1, 2}: 0.5,
		{2, 2}: 0.5,
		{3, 2}: 0.5,
	})

	if err := PreprocessFeatureMatrix(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := m.RawMatrix().Data
	stride := m.RawMatrix().Stride
	for ri := 0; ri < nRows; ri++ {
		v := data[ri*stride+2]
		if v != 0 {
			t.Errorf("row %d: expected 0 for zero-variance feature, got %v", ri, v)
		}
	}
}

// ─── NaN rejection ──────────────────────────────────────────────────────────

func TestPreprocess_RejectsNaN(t *testing.T) {
	nRows := 3
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{1, 4}: math.NaN(),
	})

	err := PreprocessFeatureMatrix(m)
	if err == nil {
		t.Fatal("expected error for NaN input, got nil")
	}
}

// ─── +Inf rejection ────────────────────────────────────────────────────────

func TestPreprocess_RejectsInf(t *testing.T) {
	nRows := 2
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 1}: math.Inf(1),
	})

	err := PreprocessFeatureMatrix(m)
	if err == nil {
		t.Fatal("expected error for +Inf input, got nil")
	}
}

func TestPreprocess_RejectsNegInf(t *testing.T) {
	nRows := 2
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 14}: math.Inf(-1),
	})

	err := PreprocessFeatureMatrix(m)
	if err == nil {
		t.Fatal("expected error for -Inf input, got nil")
	}
}

// ─── Negative value on log-transformed feature → clamped to 0 ──────────────

func TestPreprocess_NegativeClampedBeforeLog(t *testing.T) {
	nRows := 2
	// Feature 0 (TotalAttributeCount) is log-transformed.
	// A negative value should be clamped to 0 → log1p(0) = 0, not NaN.
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 0}: -5.0,
		{1, 0}: 10.0,
	})

	err := PreprocessFeatureMatrix(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no NaN crept in.
	data := m.RawMatrix().Data
	stride := m.RawMatrix().Stride
	for ri := 0; ri < nRows; ri++ {
		v := data[ri*stride+0]
		if math.IsNaN(v) {
			t.Errorf("row %d: got NaN — negative input was not clamped before log1p", ri)
		}
	}
}

// ─── In-place mutation ──────────────────────────────────────────────────────

func TestPreprocess_MutatesInPlace(t *testing.T) {
	nRows := 3
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		{0, 7}: 1.0,
		{1, 7}: 2.0,
		{2, 7}: 3.0,
	})

	dataBefore := m.RawMatrix().Data

	if err := PreprocessFeatureMatrix(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dataAfter := m.RawMatrix().Data

	if &dataBefore[0] != &dataAfter[0] {
		t.Error("PreprocessFeatureMatrix allocated a new backing slice instead of mutating in-place")
	}

	stride := m.RawMatrix().Stride
	if dataAfter[0*stride+7] == 1.0 && dataAfter[1*stride+7] == 2.0 {
		t.Error("values appear unchanged — PreprocessFeatureMatrix may not have written results back")
	}
}

// ─── Multi-feature interaction: log and non-log in same matrix ──────────────

func TestPreprocess_MixedFeatures(t *testing.T) {
	nRows := 4
	m := makeFeatureMatrix(nRows, map[[2]int]float64{
		// Feature 0 (log-transformed): wide range
		{0, 0}: 1.0,
		{1, 0}: 10.0,
		{2, 0}: 100.0,
		{3, 0}: 1000.0,
		// Feature 8 (NonScalarValueFraction, bounded, no log): narrow range
		{0, 8}: 0.1,
		{1, 8}: 0.2,
		{2, 8}: 0.3,
		{3, 8}: 0.4,
	})

	if err := PreprocessFeatureMatrix(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := m.RawMatrix().Data
	stride := m.RawMatrix().Stride

	for _, fi := range []int{0, 8} {
		sum := 0.0
		sumSq := 0.0
		for ri := 0; ri < nRows; ri++ {
			v := data[ri*stride+fi]
			sum += v
			sumSq += v * v
		}
		mean := sum / float64(nRows)
		std := math.Sqrt(sumSq/float64(nRows) - mean*mean)

		if math.Abs(mean) > 1e-10 {
			t.Errorf("feature %d: expected mean ≈ 0, got %v", fi, mean)
		}
		if math.Abs(std-1.0) > 1e-10 {
			t.Errorf("feature %d: expected std ≈ 1, got %v", fi, std)
		}
	}
}

// ─── All-zero matrix ────────────────────────────────────────────────────────

func TestPreprocess_AllZeros(t *testing.T) {
	nRows := 5
	m := mat.NewDense(nRows, NumFeatures, nil) // all zeros

	if err := PreprocessFeatureMatrix(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Every feature is constant (zero) → every output should be 0.
	data := m.RawMatrix().Data
	for i, v := range data {
		if v != 0 {
			t.Errorf("index %d: expected 0, got %v", i, v)
		}
	}
}
