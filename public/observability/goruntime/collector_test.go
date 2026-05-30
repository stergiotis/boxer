package goruntime

import (
	"runtime"
	"testing"
)

// TestCollector_ReadBasic checks a plain Read returns coherent values for the
// always-present metrics. These cannot legitimately be zero in a running Go
// process, so a zero here means the curated name drifted from the stdlib.
func TestCollector_ReadBasic(t *testing.T) {
	c := NewCollector()

	// Touch the heap so live-heap is unambiguously non-zero regardless of GC timing.
	sink := make([]byte, 1<<20)
	for i := range sink {
		sink[i] = byte(i)
	}

	var s Snapshot
	if err := c.Read(&s); err != nil {
		t.Fatalf("Read: %v", err)
	}
	runtime.KeepAlive(sink)

	if s.Goroutines == 0 {
		t.Errorf("Goroutines == 0, want >= 1")
	}
	if s.GomaxProcs == 0 {
		t.Errorf("GomaxProcs == 0, want >= 1")
	}
	if s.TotalMapped == 0 {
		t.Errorf("TotalMapped == 0, want > 0")
	}
	if s.HeapLive == 0 {
		t.Errorf("HeapLive == 0, want > 0")
	}
	// The memory classes should account for (roughly) the whole mapped total; at
	// minimum their sum must not exceed it, and objects must fit within it.
	if s.HeapObjects > s.TotalMapped {
		t.Errorf("HeapObjects %d > TotalMapped %d", s.HeapObjects, s.TotalMapped)
	}
	// /gc/pauses is a standard histogram; after at least one GC it has buckets.
	runtime.GC()
	if err := c.Read(&s); err != nil {
		t.Fatalf("Read after GC: %v", err)
	}
	if len(s.GCPauses.Buckets) == 0 {
		t.Errorf("GCPauses has no buckets after a GC")
	}
	if got, want := len(s.GCPauses.Counts), len(s.GCPauses.Buckets)-1; got != want {
		t.Errorf("GCPauses Counts=%d, want Buckets-1=%d", got, want)
	}
}

// TestCollector_AbsentMetricTolerated drives the version-tolerance path: a curated
// name absent from metrics.All() is dropped at construction, counted in Missing,
// and never reaches metrics.Read — while present names in the same set still read.
func TestCollector_AbsentMetricTolerated(t *testing.T) {
	const bogus = "/imzrt/metric/does-not-exist:bytes"
	c := newCollectorFromNames([]string{mGoroutines, bogus})

	var s Snapshot
	if err := c.Read(&s); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Missing != 1 {
		t.Errorf("Missing == %d, want 1 (the bogus metric)", s.Missing)
	}
	if s.Goroutines == 0 {
		t.Errorf("present metric (goroutines) should still be read, got 0")
	}
}

// TestCollector_GOMemLimitSentinel verifies the unset-GOMEMLIMIT path resolves to
// the documented sentinel rather than a misleading zero.
func TestCollector_GOMemLimitSentinel(t *testing.T) {
	// When the metric is entirely absent we synthesize the sentinel.
	c := newCollectorFromNames([]string{mGoroutines})
	var s Snapshot
	if err := c.Read(&s); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.GOMemLimit != MemLimitUnset {
		t.Errorf("GOMemLimit == %d, want MemLimitUnset (%d) when metric absent", s.GOMemLimit, MemLimitUnset)
	}
}

// TestCollector_SteadyStateNoAlloc asserts the observer-effect guard: after warm-up
// Read must not allocate (it reuses the sample buffer and histogram backings).
func TestCollector_SteadyStateNoAlloc(t *testing.T) {
	c := NewCollector()
	var s Snapshot
	// Warm-up reads grow the histogram backings to their steady size.
	for range 4 {
		_ = c.Read(&s)
	}
	allocs := testing.AllocsPerRun(50, func() {
		_ = c.Read(&s)
	})
	if allocs != 0 {
		t.Errorf("Read allocated %.1f times per run in steady state, want 0", allocs)
	}
}
