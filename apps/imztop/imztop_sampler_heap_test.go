package imztop

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSampler_SteadyStateHeapBounded guards the M1 done-criterion from
// ADR-0020: "running 30s does not grow Go heap (verified via
// runtime.ReadMemStats in a test)". The test drives the sampler at a
// fast cadence (10ms) for a short observation window so steady-state
// heap can be measured without a 30s wall-clock pause; the contract is
// equivalent — at sub-second intervals against a small ring, any
// per-tick allocation leak compounds visibly within a few seconds.
//
// Opt-in via IMZTOP_HEAP_TEST=1 so `go test ./...` stays fast on dev
// laptops; CI lint gate can flip the env when it wants the guard.
//
// Linux-only in practice: NewSampler needs /proc/stat and /proc/meminfo
// from the cpu/mem collectors. Other GOOS targets fail the require.NoError
// at construction — acceptable, sysmetrics already targets Linux.
func TestSampler_SteadyStateHeapBounded(t *testing.T) {
	if os.Getenv("IMZTOP_HEAP_TEST") == "" {
		t.Skip("set IMZTOP_HEAP_TEST=1 to run the M1 heap-drift guard (~3s wall time)")
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
		closeErr := s.Close()
		require.NoError(t, closeErr)
	})

	// Warm-up: let rings fill and the first published snapshot land.
	time.Sleep(1500 * time.Millisecond)
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// Observation window — ~150 publish cycles at 10ms tick interval.
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
