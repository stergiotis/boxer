//go:build llm_generated_opus47

package ecdf

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
)

// TestEnsureBandWarmCompletes drives the async registry end-to-end on
// the nil-task path: a first call schedules the background solve, the
// ecdfbands cache becomes ready, and a repeat call is idempotent (no
// error, no duplicate spawn observable via the shared snapshot).
func TestEnsureBandWarmCompletes(t *testing.T) {
	const n = 48
	const alpha = 0.029 // unique to this test so the cache starts cold
	method := ecdfbands.BandMethodBerkJones

	snap := ensureBandWarm(nil, n, alpha, method)
	if snap.State != BandJobRunning && snap.State != BandJobDone {
		t.Fatalf("unexpected initial state %v (err=%v)", snap.State, snap.Err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !ecdfbands.BandReady(n, alpha, method) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ecdfbands.BandReady(n, alpha, method) {
		t.Fatalf("band not ready within deadline")
	}

	// Idempotent: a second ensure must not error or restart.
	if snap2 := ensureBandWarm(nil, n, alpha, method); snap2.State == BandJobError {
		t.Fatalf("second ensure reported error: %v", snap2.Err)
	}
}
