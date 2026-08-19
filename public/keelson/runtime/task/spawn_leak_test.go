package task

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestSpawn_NoMonitorGoroutineLeak guards the fix for the per-task monitor
// goroutine. It previously blocked on parent.Done() alone, so a task spawned
// under a never-cancelled parent (context.Background) and completed via Done
// left its monitor goroutine alive for the process lifetime — N spawns leaked
// N goroutines. After the fix the monitor also selects on the handle's done
// channel and exits on terminal completion.
func TestSpawn_NoMonitorGoroutineLeak(t *testing.T) {
	f := newBusFixture(t)

	const n = 200
	settleGoroutines()
	base := runtime.NumGoroutine()

	for i := range n {
		h, err := Spawn(context.Background(), f.producer, SpawnOpts{Kind: "leak.test"})
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
		if err := h.Done(nil); err != nil {
			t.Fatalf("done %d: %v", i, err)
		}
	}

	// Monitor goroutines exit on handle.done; give the scheduler a moment to
	// run their final select before measuring. A leak would show ~n extra
	// goroutines; the tolerance absorbs unrelated runtime churn.
	deadline := time.Now().Add(2 * time.Second)
	for {
		settleGoroutines()
		cur := runtime.NumGoroutine()
		if cur <= base+10 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: baseline=%d, after %d spawn+done=%d (want <= baseline+10)", base, n, cur)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSpawn_ParentCancelIsAnnouncedAndWorkerTerminates verifies the
// complementary path (ADR-0188): when the parent context cancels before a
// terminal verb, the handle's Ctx cancels, the cancellation is announced on
// the bus as a task.<id>.cancel carrying CancelReasonParent, and the
// worker's own Done still publishes the terminal — observers see cancel,
// then done, instead of a task that silently vanished into "running".
func TestSpawn_ParentCancelIsAnnouncedAndWorkerTerminates(t *testing.T) {
	f := newBusFixture(t)
	obs := &recordingObserver{}
	unsub, err := WatchAll(f.observer, obs)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	h, err := Spawn(ctx, f.producer, SpawnOpts{Kind: "cancel.test"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	cancel()
	// Ctx is a child of parent, so it cancels too; wait for it.
	select {
	case <-h.Ctx().Done():
	case <-time.After(time.Second):
		t.Fatal("handle Ctx did not cancel after parent cancel")
	}
	deadline := time.Now().Add(time.Second)
	for {
		obs.mu.Lock()
		n := len(obs.cancel)
		obs.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	obs.mu.Lock()
	if len(obs.cancel) != 1 || obs.cancel[0].Reason != CancelReasonParent {
		t.Fatalf("expected one announced cancel with reason %q, got %+v", CancelReasonParent, obs.cancel)
	}
	obs.mu.Unlock()
	// The worker reacts to Cancelled() with its own terminal, which is
	// published — the handle is not terminal until then.
	if err := h.Done(nil); err != nil {
		t.Fatalf("post-cancel Done returned error: %v", err)
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.done) != 1 {
		t.Fatalf("expected the worker's done to be published after the announced cancel, got %d", len(obs.done))
	}
}

func settleGoroutines() {
	for range 3 {
		runtime.GC()
		time.Sleep(5 * time.Millisecond)
	}
}
