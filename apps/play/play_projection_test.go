package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/card"
)

func TestBucketIndexEqualWidth(t *testing.T) {
	const n = 8
	// Endpoints and midpoint.
	if got := bucketIndex(0, 0, 1, n); got != 0 {
		t.Errorf("min -> %d, want 0", got)
	}
	if got := bucketIndex(1, 0, 1, n); got != n-1 {
		t.Errorf("max -> %d, want %d", got, n-1)
	}
	if got := bucketIndex(0.5, 0, 1, n); got != 4 {
		t.Errorf("mid -> %d, want 4", got)
	}
	// Out-of-range clamps both ways.
	if got := bucketIndex(-5, 0, 1, n); got != 0 {
		t.Errorf("below min -> %d, want 0", got)
	}
	if got := bucketIndex(5, 0, 1, n); got != n-1 {
		t.Errorf("above max -> %d, want %d", got, n-1)
	}
}

// The off-by-one fix: with equal-width bins the top bucket covers a real 1/n
// slice of the range, not just the exact maximum. Under the old `*(n-1)`
// divisor, v=0.9 landed in bucket 6 and only v=1.0 reached bucket 7.
func TestBucketIndexTopBucketNotDegenerate(t *testing.T) {
	const n = 8
	if got := bucketIndex(0.9, 0, 1, n); got != 7 {
		t.Errorf("0.9 -> bucket %d, want 7 (top bucket must catch more than the exact max)", got)
	}
	// Every value in [0.875, 1.0] should map to the top bucket.
	for _, v := range []float64{0.876, 0.95, 0.999, 1.0} {
		if got := bucketIndex(v, 0, 1, n); got != n-1 {
			t.Errorf("%.3f -> bucket %d, want %d", v, got, n-1)
		}
	}
}

func TestBucketIndexDegenerate(t *testing.T) {
	if got := bucketIndex(5, 3, 3, 8); got != 0 {
		t.Errorf("zero-span -> %d, want 0", got)
	}
	if got := bucketIndex(5, 0, 10, 1); got != 0 {
		t.Errorf("n<=1 -> %d, want 0", got)
	}
}

func TestSubsampleFeaturesIdentity(t *testing.T) {
	feats := make([]card.EntityFeatures, 5)
	sampled, coordRow := subsampleFeatures(feats, 100)
	if len(sampled) != 5 || len(coordRow) != 5 {
		t.Fatalf("len sampled=%d coordRow=%d, want 5/5", len(sampled), len(coordRow))
	}
	for i := range coordRow {
		if coordRow[i] != int64(i) {
			t.Errorf("coordRow[%d]=%d, want identity", i, coordRow[i])
		}
	}
}

func TestSubsampleFeaturesCapped(t *testing.T) {
	const n, max = 1000, 100
	feats := make([]card.EntityFeatures, n)
	sampled, coordRow := subsampleFeatures(feats, max)
	if len(sampled) != max || len(coordRow) != max {
		t.Fatalf("len sampled=%d coordRow=%d, want %d", len(sampled), len(coordRow), max)
	}
	// First and last original rows must be retained, mapping monotonic.
	if coordRow[0] != 0 {
		t.Errorf("coordRow[0]=%d, want 0", coordRow[0])
	}
	if coordRow[max-1] != int64(n-1) {
		t.Errorf("coordRow[last]=%d, want %d", coordRow[max-1], n-1)
	}
	for i := 1; i < max; i++ {
		if coordRow[i] <= coordRow[i-1] {
			t.Errorf("coordRow not strictly increasing at %d: %d <= %d", i, coordRow[i], coordRow[i-1])
		}
	}
}
