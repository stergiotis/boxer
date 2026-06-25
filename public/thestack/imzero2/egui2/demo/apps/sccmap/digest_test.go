package sccmap

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/scctree"
)

// TestComputeMetricDigestTotal locks the contract renderTotals depends on:
// computeMetricDigest returns the exact sum of the per-file weights over the
// kept leaf set (the "Σ" figure) alongside the digest, and the digest's
// observation count tracks the same set. The total is accumulated in the walk
// rather than read back from the digest because a TDigest's Weight() is the
// observation count (every Push has weight 1), not the value-sum — so this
// guards that parallel accumulation, including that the keep predicate filters
// the total and the digest identically.
func TestComputeMetricDigestTotal(t *testing.T) {
	groups := []scctree.SccGroup{
		{Name: "Go", Files: []scctree.SccFile{
			{Filename: "a.go", Code: 100, Complexity: 5},
			{Filename: "b.go", Code: 250, Complexity: 12},
		}},
		{Name: "Rust", Files: []scctree.SccFile{
			{Filename: "c.rs", Code: 40, Complexity: 3, Generated: true},
		}},
	}

	// No keep predicate: every file counts. Code total = 100+250+40.
	d, total := computeMetricDigest(groups, scctree.WeightCode, nil)
	if total != 390 {
		t.Errorf("WeightCode total = %v, want 390", total)
	}
	if got := d.Count(); got != 3 {
		t.Errorf("digest Count = %d, want 3", got)
	}

	// keep dropping generated files excludes c.rs from both surfaces: Code
	// total = 100+250, two files surveyed. Mirrors App.keepFunc's predicate
	// shape (true keeps the file).
	keep := func(f *scctree.SccFile) bool { return !f.Generated }
	dk, totalk := computeMetricDigest(groups, scctree.WeightCode, keep)
	if totalk != 350 {
		t.Errorf("WeightCode total (keep) = %v, want 350", totalk)
	}
	if got := dk.Count(); got != 2 {
		t.Errorf("digest Count (keep) = %d, want 2", got)
	}

	// A different metric sums independently over the same files:
	// Complexity total = 5+12+3.
	if _, ctotal := computeMetricDigest(groups, scctree.WeightComplexity, nil); ctotal != 20 {
		t.Errorf("WeightComplexity total = %v, want 20", ctotal)
	}

	// Empty input: zero total, empty digest (no division-or-nil surprises for
	// renderTotals' humanizers).
	if d0, total0 := computeMetricDigest(nil, scctree.WeightCode, nil); total0 != 0 || d0.Count() != 0 {
		t.Errorf("empty input: total=%v count=%d, want 0/0", total0, d0.Count())
	}
}
