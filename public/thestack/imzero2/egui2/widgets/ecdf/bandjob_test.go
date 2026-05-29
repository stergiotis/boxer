//go:build llm_generated_opus47

package ecdf

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
)

// TestEnsureBandWarmCompletes drives the async registry end-to-end on
// the nil-task path: a first call schedules the background solve, the
// ecdfbands cache becomes ready, and a repeat call under the same job
// key is idempotent (no error, no duplicate spawn observable via the
// shared snapshot).
func TestEnsureBandWarmCompletes(t *testing.T) {
	const jobKey = "test-completes"
	const n = 48
	const alpha = 0.029 // unique to this test so the cache starts cold
	method := ecdfbands.BandMethodBerkJones

	snap := ensureBandWarm(jobKey, nil, n, alpha, method)
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

	// Idempotent: a second ensure under the same key must not error or restart.
	if snap2 := ensureBandWarm(jobKey, nil, n, alpha, method); snap2.State == BandJobError {
		t.Fatalf("second ensure reported error: %v", snap2.Err)
	}
}

// TestCancelBandJobRemovesEntry pins the cancellation contract the
// inspector relies on when its window is closed or retracted:
// cancelBandJob aborts the in-flight warm-up and removes its registry
// entry, so a later reopen schedules a fresh solve rather than reading
// the cancelled one. Cancelling an absent key is a no-op.
func TestCancelBandJobRemovesEntry(t *testing.T) {
	const jobKey = "test-cancel"
	const n = 4096      // large enough that the solve cannot finish before we cancel
	const alpha = 0.013 // unique so the cache starts cold
	method := ecdfbands.BandMethodBerkJones

	snap := ensureBandWarm(jobKey, nil, n, alpha, method)
	if snap.State == BandJobError {
		t.Fatalf("scheduling reported an error: %v", snap.Err)
	}
	if _, ok := bandJobs.Load(jobKey); !ok {
		t.Fatalf("job was not registered under %q", jobKey)
	}

	cancelBandJob(jobKey)
	if _, ok := bandJobs.Load(jobKey); ok {
		t.Fatalf("cancelBandJob left a registry entry under %q", jobKey)
	}

	// Idempotent: cancelling an already-forgotten or never-seen key must
	// not panic.
	cancelBandJob(jobKey)
	cancelBandJob("never-scheduled")
}

// TestEnsureBandWarmReschedulesOnParamChange confirms that when the
// parameters registered under a job key move on (the live digest grew,
// advancing n) the stale solve is replaced rather than reused: the entry
// under the key becomes a distinct job carrying the new n.
func TestEnsureBandWarmReschedulesOnParamChange(t *testing.T) {
	const jobKey = "test-reschedule"
	const alpha = 0.017 // unique so the cache starts cold
	method := ecdfbands.BandMethodBerkJones

	_ = ensureBandWarm(jobKey, nil, 64, alpha, method)
	first, ok := bandJobs.Load(jobKey)
	if !ok {
		t.Fatalf("first job not registered")
	}
	if got := first.(*bandJob).n; got != 64 {
		t.Fatalf("first job n = %d, want 64", got)
	}

	// Same key, advanced n: the stale 64-solve must be cancelled and a
	// fresh job for the new n registered in its place.
	_ = ensureBandWarm(jobKey, nil, 128, alpha, method)
	second, ok := bandJobs.Load(jobKey)
	if !ok {
		t.Fatalf("rescheduled job not registered")
	}
	if got := second.(*bandJob).n; got != 128 {
		t.Fatalf("rescheduled job n = %d, want 128", got)
	}
	if first == second {
		t.Fatalf("expected a distinct job instance after the parameter change")
	}

	cancelBandJob(jobKey) // forget the live goroutine before the test ends
}
