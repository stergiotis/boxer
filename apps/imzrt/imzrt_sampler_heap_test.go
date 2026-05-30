package imzrt

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSampler_SteadyStateHeapBounded guards the M1 done-criterion from ADR-0061:
// the sampler must not grow the Go heap in steady state. It drives the sampler at
// a fast cadence for a short window so a per-tick allocation leak compounds
// visibly within seconds; the published-snapshot path allocates bounded per-tick
// garbage (fresh slices + struct) that GC reclaims, so live HeapAlloc must stay
// flat.
//
// This is the self-measuring app measuring itself — the very property the
// dashboard is about — so the guard matters more here than for imztop.
//
// Opt-in via IMZRT_HEAP_TEST=1 so `go test ./...` stays fast. Unlike imztop's
// equivalent this is portable: goruntime.NewCollector needs no /proc, so the test
// runs on every GOOS.
func TestSampler_SteadyStateHeapBounded(t *testing.T) {
	if os.Getenv("IMZRT_HEAP_TEST") == "" {
		t.Skip("set IMZRT_HEAP_TEST=1 to run the M1 heap-drift guard (~3s wall time)")
	}

	s, err := NewSampler(SamplerOptions{
		UpdateInterval: 10 * time.Millisecond,
		HistoryWindow:  1 * time.Second,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)
	t.Cleanup(func() {
		require.NoError(t, s.Close())
	})

	// Warm-up: let the rings fill and the first snapshots publish.
	time.Sleep(1500 * time.Millisecond)
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Observation window — ~150 publish cycles at the 10ms tick interval.
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
