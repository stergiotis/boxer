package bgjob

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// endingApi hands out handles that record how the run's task ended.
type endingApi struct {
	task.TaskApiI
	mu    sync.Mutex
	ends  []string
	ended chan struct{}
}

func newEndingApi() *endingApi { return &endingApi{ended: make(chan struct{}, 4)} }

func (a *endingApi) InstanceKey() uint64 { return 0 }

func (a *endingApi) Spawn(ctx context.Context, _ task.SpawnOpts) (task.HandleI, error) {
	return &endingHandle{api: a, ctx: ctx}, nil
}

func (a *endingApi) first() (end string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.ends) > 0 {
		end = a.ends[0]
	}
	return
}

type endingHandle struct {
	task.HandleI
	api  *endingApi
	ctx  context.Context
	once sync.Once
}

func (h *endingHandle) end(how string) {
	h.once.Do(func() {
		h.api.mu.Lock()
		h.api.ends = append(h.api.ends, how)
		h.api.mu.Unlock()
		h.api.ended <- struct{}{}
	})
}

func (h *endingHandle) Ctx() context.Context             { return h.ctx }
func (h *endingHandle) Report(task.ProgressReport)       {}
func (h *endingHandle) Done([]byte) error                { h.end("done"); return nil }
func (h *endingHandle) Error(error, string) (rerr error) { h.end("error"); return nil }

// A panic in compute is the run's error: the runner ends Failed and the task
// as an error, never as a success recorded on the way out of the panic.
func TestRunnerPanicEndsTheRunAsAnError(t *testing.T) {
	for _, reporting := range []bool{false, true} {
		api := newEndingApi()
		var r Runner[int]
		if reporting {
			r.StartReporting(api, testSpec(), func(context.Context, Reporter) (*int, error) { panic("boom") })
		} else {
			r.Start(api, testSpec(), func(context.Context) (*int, error) { panic("boom") })
		}
		if st := waitState(t, &r, 3*time.Second); st != StateFailed {
			t.Fatalf("reporting=%v: terminal state = %d, want StateFailed", reporting, st)
		}
		if r.Snapshot().Err == nil {
			t.Errorf("reporting=%v: the failed run carries no error", reporting)
		}
		<-api.ended
		if got := api.first(); got != "error" {
			t.Errorf("reporting=%v: the task ended as %q, want error", reporting, got)
		}
	}
}

func TestKeyedPanicIsTheRunsError(t *testing.T) {
	api := newEndingApi()
	var k Keyed[int]
	k.Configure(api, "k", "keyed")
	_, _, _, _ = k.Demand("q", func(context.Context) (int, error) { panic("boom") })
	if _, err := settled(t, &k, "q"); err == nil {
		t.Fatal("a panicking run settled without an error")
	}
	<-api.ended
	if got := api.first(); got != "error" {
		t.Errorf("the task ended as %q, want error", got)
	}
}
