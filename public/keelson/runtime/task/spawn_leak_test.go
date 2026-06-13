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

	for i := 0; i < n; i++ {
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

// TestSpawn_ParentCancelTerminates verifies the complementary path: when the
// parent context cancels before a terminal verb, the handle is marked
// terminal (so a later Done no-ops) and the monitor goroutine exits.
func TestSpawn_ParentCancelTerminates(t *testing.T) {
	f := newBusFixture(t)
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
	// A post-cancel Done must be a no-op (handle already terminal): it
	// returns nil without publishing.
	if err := h.Done(nil); err != nil {
		t.Fatalf("post-cancel Done returned error: %v", err)
	}
}

func settleGoroutines() {
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(5 * time.Millisecond)
	}
}
