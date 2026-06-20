package imztop

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// TestSampler_SteadyStateHeapBounded guards the M1 done-criterion from
// ADR-0020: running does not grow the Go heap (verified via runtime.ReadMemStats).
// It drives the consumer Sampler — fed by a co-located scraper — at a fast
// cadence for a short window so any per-frame allocation leak compounds
// visibly. Opt-in via IMZTOP_HEAP_TEST=1 so `go test ./...` stays fast.
//
// Linux-only in practice: the scraper needs /proc; newColocatedSampler skips
// otherwise.
func TestSampler_SteadyStateHeapBounded(t *testing.T) {
	if os.Getenv("IMZTOP_HEAP_TEST") == "" {
		t.Skip("set IMZTOP_HEAP_TEST=1 to run the M1 heap-drift guard (~3s wall time)")
	}

	// The Sampler stays alive via the bus subscription + t.Cleanup teardown.
	_ = newColocatedSampler(t, SamplerOptions{
		UpdateInterval: 10 * time.Millisecond,
		HistoryWindow:  1 * time.Second,
	})

	// Warm-up: let rings fill and the first published snapshot land.
	time.Sleep(1500 * time.Millisecond)
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Observation window — ~150 publish cycles at 10ms cadence.
	time.Sleep(1500 * time.Millisecond)
	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	const maxGrowth int64 = 1 << 20 // 1 MiB slack for GC pacing / scheduler variance
	if growth > maxGrowth {
		t.Fatalf("heap grew %d bytes during steady state (cap %d); before=%d after=%d",
			growth, maxGrowth, before.HeapAlloc, after.HeapAlloc)
	}
}
