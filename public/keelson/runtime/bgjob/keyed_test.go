package bgjob

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcancel"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskcreated"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskdone"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskprogress"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("not reached: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// settled polls key until its run is over.
func settled[T any](t *testing.T, k *Keyed[T], key string) (val T, err error) {
	t.Helper()
	eventually(t, "the run ends", func() bool {
		var done, busy bool
		val, done, err, busy = k.Demand(key, nil)
		return done && !busy
	})
	return
}

func TestKeyedRunsOncePerKeyAndSupersedes(t *testing.T) {
	var k Keyed[int]
	var runs atomic.Int32
	slow := func(ctx context.Context) (int, error) {
		runs.Add(1)
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return 7, nil
		}
	}
	if _, done, _, busy := k.Demand("nobody-asked", nil); done || busy {
		t.Error("a nil run is a question: it starts nothing")
	}
	if _, done, _, busy := k.Demand("a", slow); done || !busy {
		t.Errorf("first demand: done %v busy %v", done, busy)
	}
	k.Demand("a", slow)
	eventually(t, "one run for a", func() bool { return runs.Load() == 1 })
	if runs.Load() != 1 {
		t.Error("the same key does not start a second run")
	}
	// A new key supersedes: the old result never lands.
	k.Demand("b", slow)
	if v, err := settled(t, &k, "b"); err != nil || v != 7 {
		t.Errorf("b: %v, %v", v, err)
	}
	k.Demand("b", slow)
	if runs.Load() != 2 {
		t.Errorf("runs: got %d, want 2 — a done key is a cache hit", runs.Load())
	}
	if st := k.Snapshot().State; st != StateDone {
		t.Errorf("state: got %v, want done", st)
	}
	k.Invalidate()
	k.Demand("b", slow)
	eventually(t, "Invalidate runs the key again", func() bool { return runs.Load() == 3 })
}

func TestKeyedReleasesWhatItOwns(t *testing.T) {
	var k Keyed[string]
	var mu sync.Mutex
	var released []string
	k.SetDispose(func(v string) {
		mu.Lock()
		released = append(released, v)
		mu.Unlock()
	})
	got := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), released...)
	}
	gate := make(chan struct{})
	k.Demand("slow", func(ctx context.Context) (string, error) {
		<-gate
		return "superseded", nil
	})
	k.Demand("fast", func(context.Context) (string, error) { return "held", nil })
	settled(t, &k, "fast")
	close(gate)
	eventually(t, "a superseded run's value is released", func() bool {
		return len(got()) == 1 && got()[0] == "superseded"
	})
	k.Close()
	if r := got(); len(r) != 2 || r[1] != "held" {
		t.Errorf("released: %v, want the held value released on Close", r)
	}
}

func TestKeyedCancelIsAnAnswer(t *testing.T) {
	var k Keyed[int]
	var runs atomic.Int32
	blocked := func(ctx context.Context) (int, error) {
		runs.Add(1)
		<-ctx.Done()
		return 0, ctx.Err()
	}
	k.Demand("q", blocked)
	eventually(t, "running", func() bool { return runs.Load() == 1 })
	k.Cancel()
	if _, err := settled(t, &k, "q"); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err: got %v, want ErrCancelled", err)
	}
	if snap := k.Snapshot(); snap.State != StateFailed || !errors.Is(snap.Err, ErrCancelled) {
		t.Errorf("snapshot: %+v", snap)
	}
	k.Demand("q", blocked)
	time.Sleep(10 * time.Millisecond)
	if runs.Load() != 1 {
		t.Error("a cancelled key is not started again by the next demand")
	}
	k.Invalidate()
	k.Demand("q", blocked)
	eventually(t, "Invalidate lets it run again", func() bool { return runs.Load() == 2 })
	k.Close()
}

func TestKeyedReportingDrivesSnapshot(t *testing.T) {
	var k Keyed[int]
	step := make(chan struct{})
	k.DemandReporting("p", func(ctx context.Context, report Reporter) (int, error) {
		report(0, 0, "asking")
		<-step
		report(5, 10, "half")
		<-step
		return 1, nil
	})
	eventually(t, "indeterminate first", func() bool {
		s := k.Snapshot()
		return s.State == StateRunning && s.Fraction < 0 && s.Note == "asking"
	})
	step <- struct{}{}
	eventually(t, "then the reported share", func() bool {
		s := k.Snapshot()
		return s.Fraction == 0.5 && s.Note == "half"
	})
	step <- struct{}{}
	settled(t, &k, "p")
}

// createdObserver records the tasks it sees created.
type createdObserver struct {
	mu      sync.Mutex
	created []taskcreated.TaskCreated
}

func (o *createdObserver) OnCreated(c taskcreated.TaskCreated) {
	o.mu.Lock()
	o.created = append(o.created, c)
	o.mu.Unlock()
}
func (o *createdObserver) OnProgress(taskprogress.TaskProgress) {}
func (o *createdObserver) OnDone(taskdone.TaskDone)             {}
func (o *createdObserver) OnError(taskerror.TaskError)          {}
func (o *createdObserver) OnCancel(taskcancel.TaskCancel)       {}

func (o *createdObserver) first() (c taskcreated.TaskCreated, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.created) == 0 {
		return
	}
	return o.created[0], true
}

func TestKeyedRunIsATaskTheBusCanCancel(t *testing.T) {
	busInst := inprocbus.NewInst(zerolog.Nop())
	api := task.NewBusApi(task.ApiConfig{Bus: busInst.NewClient("test.app", task.ProducerCaps()), AppId: "test.app"})
	watcher := busInst.NewClient("test.observer", append(task.ObserverCaps(), task.CancelerCaps()...))
	obs := &createdObserver{}
	unsub, err := task.WatchAll(watcher, obs)
	if err != nil {
		t.Fatalf("WatchAll: %v", err)
	}
	defer unsub()

	var k Keyed[int]
	k.Configure(api, "test-find", "find in the store")
	k.Demand("q", func(ctx context.Context) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})
	eventually(t, "the run is announced", func() bool { _, ok := obs.first(); return ok })
	c, _ := obs.first()
	if c.Kind != "test-find" || c.Title != "find in the store" || !c.CancellableB {
		t.Errorf("created: %+v", c)
	}
	if err = task.RequestCancel(watcher, task.TaskIdT(c.TaskId), "test"); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if _, err = settled(t, &k, "q"); !errors.Is(err, ErrCancelled) {
		t.Errorf("err: got %v, want ErrCancelled — a cancel from the bus is a cancel", err)
	}
}
