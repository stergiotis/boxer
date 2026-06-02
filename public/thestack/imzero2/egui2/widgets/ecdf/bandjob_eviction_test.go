package ecdf

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
)

// TestEnsureBandWarmEvictsOnDone pins the registry self-eviction that plugs
// the orphaned-job leak: once a warm-up completes (cache populated) its
// bandJobs entry is removed even though no inspector closed, and a repeat
// ensure under the same key reports Done from the cache without registering
// or spawning a redundant job.
func TestEnsureBandWarmEvictsOnDone(t *testing.T) {
	const jobKey = "test-evict-on-done"
	const n = 64
	const alpha = 0.023 // unique so the cache starts cold
	method := ecdfbands.BandMethodBerkJones

	ensureBandWarm(jobKey, nil, n, alpha, method)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !ecdfbands.BandReady(n, alpha, method) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ecdfbands.BandReady(n, alpha, method) {
		t.Fatalf("band not ready within deadline")
	}

	// The eviction (CompareAndDelete) runs just after the cache write; poll
	// briefly for the entry to disappear.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := bandJobs.Load(jobKey); !ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := bandJobs.Load(jobKey); ok {
		t.Fatalf("completed job was not evicted from the registry")
	}

	// A subsequent ensure is a pure cache hit: Done, and it must not register
	// a fresh job under the key.
	snap := ensureBandWarm(jobKey, nil, n, alpha, method)
	if snap.State != BandJobDone {
		t.Fatalf("post-cache ensure: state %v, want BandJobDone", snap.State)
	}
	if _, ok := bandJobs.Load(jobKey); ok {
		t.Fatalf("cache-hit ensure registered a job; expected none")
	}
}
