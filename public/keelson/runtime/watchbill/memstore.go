package watchbill

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// MemStore is [StoreI] with no server: the test double for the worker and
// for every consumer's tests. Its semantics are the SQL's — a claim wins
// exactly when the row was queued and due, a transition lands exactly when
// its guards hold — under one mutex, so the race a server serialises is
// serialised here too.
type MemStore struct {
	mu     sync.Mutex
	jobs   map[string]watchbillstore.Job
	events []MemEvent
}

// MemEvent is one event row with its instant.
type MemEvent struct {
	At    time.Time
	Event watchbillstore.Event
}

var _ StoreI = (*MemStore)(nil)

// NewMemStore returns an empty store.
func NewMemStore() (inst *MemStore) {
	inst = &MemStore{jobs: make(map[string]watchbillstore.Job, 16)}
	return
}

func (inst *MemStore) Enqueue(_ context.Context, job watchbillstore.Job) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.jobs[job.ID]; dup {
		return eh.Errorf("duplicate job id")
	}
	inst.jobs[job.ID] = job
	return
}

func (inst *MemStore) Queue(_ context.Context, kinds []string, now time.Time, limit int) (ids []string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	var due []watchbillstore.Job
	for _, j := range inst.jobs {
		if j.State != watchbillstore.StateQueued || j.RunAfter.After(now) {
			continue
		}
		if len(kinds) > 0 && !slices.Contains(kinds, j.Kind) {
			continue
		}
		due = append(due, j)
	}
	sort.SliceStable(due, func(a, b int) bool {
		if due[a].Priority != due[b].Priority {
			return due[a].Priority < due[b].Priority
		}
		if !due[a].RunAfter.Equal(due[b].RunAfter) {
			return due[a].RunAfter.Before(due[b].RunAfter)
		}
		return due[a].ID < due[b].ID
	})
	for _, j := range due {
		if limit > 0 && len(ids) >= limit {
			break
		}
		ids = append(ids, j.ID)
	}
	return
}

func (inst *MemStore) Claim(_ context.Context, id string, workerRun string, now time.Time) (job watchbillstore.Job, won bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	job, found := inst.jobs[id]
	if !found {
		return
	}
	if job.State == watchbillstore.StateQueued && !job.RunAfter.After(now) {
		job.State = watchbillstore.StateRunning
		job.WorkerRun = workerRun
		job.Attempt++
		inst.jobs[id] = job
	}
	won = job.State == watchbillstore.StateRunning && job.WorkerRun == workerRun
	return
}

func (inst *MemStore) Get(_ context.Context, id string) (job watchbillstore.Job, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	job, found = inst.jobs[id]
	return
}

func (inst *MemStore) Transition(_ context.Context, t Transition) (job watchbillstore.Job, ok bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	job, found := inst.jobs[t.ID]
	if !found {
		return
	}
	guard := (len(t.From) == 0 || slices.Contains(t.From, job.State)) && (t.HeldBy == "" || job.WorkerRun == t.HeldBy)
	if guard {
		job.State = t.To
		job.WorkerRun = t.Actor
		if t.RunAfter != nil {
			job.RunAfter = *t.RunAfter
		}
		if t.FinishedAt != nil {
			job.FinishedAt = *t.FinishedAt
		}
		if t.LastError != nil {
			job.LastError = *t.LastError
		}
		if t.ResetAttempts {
			job.Attempt = 0
		}
		inst.jobs[t.ID] = job
	}
	ok = job.State == t.To && job.WorkerRun == t.Actor
	return
}

func (inst *MemStore) Held(_ context.Context, workerRun string) (jobs []watchbillstore.Job, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, j := range inst.jobs {
		if slices.Contains(heldStates, j.State) && (workerRun == "" || j.WorkerRun == workerRun) {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(a, b int) bool { return jobs[a].ID < jobs[b].ID })
	return
}

func (inst *MemStore) WriteEvent(_ context.Context, at time.Time, ev watchbillstore.Event) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.events = append(inst.events, MemEvent{At: at, Event: ev})
	return
}

func (inst *MemStore) Expire(_ context.Context, cutoff time.Time) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for id, j := range inst.jobs {
		if watchbillstore.IsFinal(j.State) && j.FinishedAt.Before(cutoff) {
			delete(inst.jobs, id)
		}
	}
	return
}

// Events returns the event rows of one job in write order, or every job's
// when id is empty. A copy.
func (inst *MemStore) Events(id string) (out []MemEvent) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, e := range inst.events {
		if id == "" || e.Event.ID == id {
			out = append(out, e)
		}
	}
	return
}

// Jobs returns every job row, sorted by id. A copy.
func (inst *MemStore) Jobs() (out []watchbillstore.Job) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = make([]watchbillstore.Job, 0, len(inst.jobs))
	for _, j := range inst.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return
}
