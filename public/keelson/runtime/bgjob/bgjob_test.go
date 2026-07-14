package bgjob

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testSpec() Spec {
	return Spec{
		Kind:       "test.job",
		Title:      "test",
		Tag:        "target-a",
		StageNotes: []string{"one", "two", "three"},
		StageDelay: 10 * time.Millisecond,
	}
}

// waitState polls until the runner leaves StateRunning or the deadline
// passes, returning the terminal state.
func waitState(t *testing.T, r *Runner[int], d time.Duration) StateE {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if st := r.Snapshot().State; st != StateRunning {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("runner did not finish within %s", d)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestRunnerPublishesResultOnce exercises the worker→render-thread handoff.
// Run with -race: the worker writes under the mutex while this loop polls
// Snapshot, so the detector validates the handoff.
func TestRunnerPublishesResultOnce(t *testing.T) {
	var r Runner[int]
	if !r.Start(nil, testSpec(), func(context.Context) (*int, error) {
		v := 42
		return &v, nil
	}) {
		t.Fatal("Start returned false on an idle runner")
	}
	if st := waitState(t, &r, 3*time.Second); st != StateDone {
		t.Fatalf("terminal state = %d, want StateDone", st)
	}
	res, tag, ok := r.TakeResult()
	if !ok || res == nil || *res != 42 || tag != "target-a" {
		t.Fatalf("TakeResult = (%v, %q, %v), want (42, target-a, true)", res, tag, ok)
	}
	if _, _, ok := r.TakeResult(); ok {
		t.Error("TakeResult succeeded twice — result must be consume-once")
	}
	if r.Snapshot().State != StateIdle {
		t.Error("runner not idle after result consumed")
	}
}

// TestRunnerSecondStartRejectedWhileRunning verifies the in-flight guard.
func TestRunnerSecondStartRejectedWhileRunning(t *testing.T) {
	var r Runner[int]
	r.Start(nil, testSpec(), func(context.Context) (*int, error) {
		v := 1
		return &v, nil
	})
	if r.Start(nil, testSpec(), func(context.Context) (*int, error) {
		t.Error("competing worker ran")
		return nil, nil
	}) {
		t.Error("second Start accepted while a run was in flight")
	}
	waitState(t, &r, 3*time.Second)
}

// TestRunnerInvalidateDropsResult verifies that invalidating an in-flight
// run causes the worker to drop its result instead of publishing it.
func TestRunnerInvalidateDropsResult(t *testing.T) {
	var r Runner[int]
	spec := testSpec()
	r.Start(nil, spec, func(context.Context) (*int, error) {
		v := 7
		return &v, nil
	})
	r.Invalidate()

	// Give the worker time to finish its staged compute and attempt to
	// publish.
	time.Sleep(time.Duration(len(spec.StageNotes))*spec.StageDelay + 200*time.Millisecond)
	if _, _, ok := r.TakeResult(); ok {
		t.Error("invalidated run published a result anyway")
	}
	if r.Snapshot().State == StateRunning {
		t.Error("runner still running after invalidate")
	}
}

// TestRunnerFailureSurfacesError verifies the failed state carries the
// compute error for the render thread.
func TestRunnerFailureSurfacesError(t *testing.T) {
	var r Runner[int]
	boom := errors.New("boom")
	r.Start(nil, testSpec(), func(context.Context) (*int, error) {
		return nil, boom
	})
	if st := waitState(t, &r, 3*time.Second); st != StateFailed {
		t.Fatalf("terminal state = %d, want StateFailed", st)
	}
	if snap := r.Snapshot(); !errors.Is(snap.Err, boom) {
		t.Errorf("Snapshot().Err = %v, want the compute error", snap.Err)
	}
	if _, _, ok := r.TakeResult(); ok {
		t.Error("failed run yielded a result")
	}
}

// TestRunnerCancel verifies cooperative cancellation resets to idle.
func TestRunnerCancel(t *testing.T) {
	var r Runner[int]
	spec := testSpec()
	spec.StageDelay = 100 * time.Millisecond
	r.Start(nil, spec, func(context.Context) (*int, error) {
		v := 9
		return &v, nil
	})
	r.Cancel()
	if st := waitState(t, &r, 3*time.Second); st != StateIdle {
		t.Fatalf("terminal state after cancel = %d, want StateIdle", st)
	}
	if _, _, ok := r.TakeResult(); ok {
		t.Error("cancelled run yielded a result")
	}
}

// TestStartReportingDrivesSnapshot verifies the reporting variant: the
// snapshot starts indeterminate, follows determinate reports (fraction +
// note), returns to indeterminate on a total==0 phase, and completes.
func TestStartReportingDrivesSnapshot(t *testing.T) {
	var r Runner[int]
	phase := make(chan struct{})   // compute waits here between reports
	inspect := make(chan Snapshot) // test hands back what it observed
	if !r.StartReporting(nil, Spec{Kind: "test.rep", Title: "rep", Tag: "target-b"},
		func(ctx context.Context, report Reporter) (*int, error) {
			step := func(done, total uint64, note string) Snapshot {
				report(done, total, note)
				phase <- struct{}{}
				return <-inspect
			}
			if snap := step(0, 0, "warming"); snap.Fraction >= 0 || snap.Note != "warming" {
				t.Errorf("indeterminate snapshot = %+v", snap)
			}
			if snap := step(25, 100, "crunching"); snap.Fraction != 0.25 || snap.Note != "crunching" {
				t.Errorf("determinate snapshot = %+v", snap)
			}
			if snap := step(0, 0, "finalizing"); snap.Fraction >= 0 || snap.EtaMs != 0 {
				t.Errorf("post-phase snapshot = %+v", snap)
			}
			v := 11
			return &v, nil
		}) {
		t.Fatal("StartReporting returned false on an idle runner")
	}
	for range 3 {
		<-phase
		inspect <- r.Snapshot()
	}
	if st := waitState(t, &r, 3*time.Second); st != StateDone {
		t.Fatalf("terminal state = %d, want StateDone", st)
	}
	if res, tag, ok := r.TakeResult(); !ok || *res != 11 || tag != "target-b" {
		t.Fatalf("TakeResult = (%v, %q, %v)", res, tag, ok)
	}
}

// TestStartReportingCancelResetsIdle verifies a cancelled reporting run
// resets to idle instead of surfacing the compute's context error as a
// failure (there are no trailing pacing stages to absorb it).
func TestStartReportingCancelResetsIdle(t *testing.T) {
	var r Runner[int]
	started := make(chan struct{})
	r.StartReporting(nil, Spec{Kind: "test.rep", Title: "rep"},
		func(ctx context.Context, report Reporter) (*int, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err() // what a subprocess shell-out surfaces on kill
		})
	<-started
	r.Cancel()
	if st := waitState(t, &r, 3*time.Second); st != StateIdle {
		t.Fatalf("terminal state after cancel = %d, want StateIdle", st)
	}
	if _, _, ok := r.TakeResult(); ok {
		t.Error("cancelled run yielded a result")
	}
}
