package task

import (
	"bytes"
	"context"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"
)

// monitorCreator is the function that starts the per-task monitor goroutine.
// The goroutineleak profile's debug=2 rendering names it on the "created by"
// line, which is what makes the assertion below specific to this package's
// monitor rather than to whatever else the test binary has running.
const monitorCreator = "github.com/stergiotis/boxer/public/keelson/runtime/task.spawnWithCancel"

// TestSpawn_NoMonitorGoroutineLeak guards the fix for the per-task monitor
// goroutine. It previously blocked on parent.Done() alone, so a task spawned
// under a never-cancelled parent (context.Background) and completed via Done
// left its monitor goroutine alive for the process lifetime — N spawns leaked
// N goroutines. After the fix the monitor also selects on the handle's done
// channel and exits on terminal completion.
//
// The assertion is the go1.27 goroutineleak profile (ADR-0199): a goroutine
// blocked on a concurrency primitive that can no longer unblock. Until then
// this test counted runtime.NumGoroutine() against a baseline with a slack of
// ten, which could only see a leak that was numerous and could not say whose
// it was. The profile names the leaked stack, so one leaked monitor fails —
// and the count of tasks below no longer carries the assertion, it only
// reproduces the original shape.
func TestSpawn_NoMonitorGoroutineLeak(t *testing.T) {
	f := newBusFixture(t)

	const n = 200
	for i := range n {
		h, err := Spawn(context.Background(), f.producer, SpawnOpts{Kind: "leak.test"})
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
		if err := h.Done(nil); err != nil {
			t.Fatalf("done %d: %v", i, err)
		}
	}

	// The monitors exit on handle.done. Give the scheduler their final select
	// before asking: a goroutine that is *about* to exit is not leaked, but it
	// is still blocked, and the profile would report it.
	settleGoroutines()

	leaked := leakedGoroutinesCreatedBy(t, monitorCreator)
	if len(leaked) != 0 {
		t.Fatalf("%d monitor goroutine(s) leaked after %d spawn+done; first stack:\n%s",
			len(leaked), n, leaked[0])
	}
}

// The zero above means nothing unless the instrument can report a non-zero, so
// this leaks one goroutine on purpose and asserts it is seen. The leak is
// permanent for the rest of the test binary — one goroutine blocked on a
// channel nobody holds — which is the price of knowing the check works.
// It is created by leakOneForever, so it does not match monitorCreator.
func TestGoroutineLeakProfileReportsALeak(t *testing.T) {
	leakOneForever()
	settleGoroutines()

	const creator = "github.com/stergiotis/boxer/public/keelson/runtime/task.leakOneForever"
	leaked := leakedGoroutinesCreatedBy(t, creator)
	if len(leaked) == 0 {
		t.Fatal("the goroutineleak profile reported no leak for a goroutine deliberately blocked on an unreachable channel")
	}
	if !strings.Contains(leaked[0], "(leaked)") {
		t.Errorf("expected the leaked-state annotation in the stack, got:\n%s", leaked[0])
	}
}

func leakOneForever() {
	ch := make(chan struct{})
	go func() { <-ch }() // ch goes out of scope here: nobody can ever send or close
}

// leakedGoroutinesCreatedBy returns the goroutineleak profile's stack records
// for goroutines started by createdBy.
//
// Two properties of the profile shape this. Its Count() is 0 — the leak set is
// computed by a GC-assisted reachability pass at WriteTo time, not maintained
// as the program runs — so the profile has to be written to be read. And
// debug=2 is the rendering that carries both the "created by" attribution and
// the "(leaked)" state; debug=1 folds stacks and drops both.
func leakedGoroutinesCreatedBy(t *testing.T, createdBy string) (out []string) {
	t.Helper()
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		t.Fatal("no goroutineleak profile: this needs go1.27 or newer (ADR-0199)")
	}
	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 2); err != nil {
		t.Fatalf("write goroutineleak profile: %v", err)
	}
	for _, rec := range strings.Split(buf.String(), "\n\n") {
		if strings.Contains(rec, "created by "+createdBy) {
			out = append(out, strings.TrimSpace(rec))
		}
	}
	return
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
