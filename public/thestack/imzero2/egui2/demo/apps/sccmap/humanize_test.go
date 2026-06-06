package sccmap

import "testing"

func TestHumanizeCount(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{-5, "0"}, // negatives clamp
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1k"}, // trailing ".0" trimmed
		{1500, "1.5k"},
		{12000, "12k"},
		{999000, "999k"},
		{1_000_000, "1M"},
		{1_500_000, "1.5M"},
		{2_000_000_000, "2G"},
		{3_400_000_000, "3.4G"},
	}
	for _, tc := range cases {
		if got := humanizeCount(tc.in); got != tc.want {
			t.Errorf("humanizeCount(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHumanizeBytes(t *testing.T) {
	// Delegates to dustin/go-humanize (decimal SI). Assert the stable,
	// obvious shapes rather than every rounding edge of the dependency.
	if got := humanizeBytes(-1); got != "0 B" {
		t.Errorf("humanizeBytes(-1) = %q, want %q", got, "0 B")
	}
	if got := humanizeBytes(0); got != "0 B" {
		t.Errorf("humanizeBytes(0) = %q, want %q", got, "0 B")
	}
	if got := humanizeBytes(1500); got != "1.5 kB" {
		t.Errorf("humanizeBytes(1500) = %q, want %q", got, "1.5 kB")
	}
}

// TestSccMetricsHaveHumanizers guards against a future metric being added
// to the registry without a Humanize func — makeCellLabelFn calls it
// unconditionally, so a nil would panic at render time.
func TestSccMetricsHaveHumanizers(t *testing.T) {
	for _, m := range sccMetrics {
		if m.Humanize == nil {
			t.Errorf("metric %q has a nil Humanize func", m.Name)
		}
	}
}
