package watchbill

import (
	"context"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// StoreI is the seam between the worker and the job table (ADR-0223 §SD3).
// Every mutation is a guarded change followed by a read-back, and the
// read-back is the answer: a caller holds what it asked for exactly when
// the row says so. Safe for concurrent use.
//
// The worker-run column names the run that last changed the row — while
// the row is running, the run that holds it — and every transition writes
// the acting run into it, so two actors driving one row to the same state
// still get exactly one winner.
type StoreI interface {
	// Enqueue inserts job as written and flushes.
	Enqueue(ctx context.Context, job watchbillstore.Job) (err error)
	// Queue lists the ids a worker may take now: queued, due, of one of
	// kinds (empty: any), oldest-first within priority, at most limit.
	Queue(ctx context.Context, kinds []string, now time.Time, limit int) (ids []string, err error)
	// Claim takes a queued, due job for workerRun. won is true exactly
	// when the row read back names workerRun.
	Claim(ctx context.Context, id string, workerRun string, now time.Time) (job watchbillstore.Job, won bool, err error)
	// Get reads one job.
	Get(ctx context.Context, id string) (job watchbillstore.Job, found bool, err error)
	// Transition applies t and reads back; ok is true exactly when the
	// row is in t.To and names t.Actor.
	Transition(ctx context.Context, t Transition) (job watchbillstore.Job, ok bool, err error)
	// Held lists the jobs a run holds: running, or running with a cancel
	// requested. Empty run lists every held job.
	Held(ctx context.Context, workerRun string) (jobs []watchbillstore.Job, err error)
	// WriteEvent appends one transition row at the given instant and
	// flushes.
	WriteEvent(ctx context.Context, at time.Time, ev watchbillstore.Event) (err error)
	// Expire deletes the rows that left the queue before cutoff.
	Expire(ctx context.Context, cutoff time.Time) (err error)
}

// Transition is one guarded change of a job row. From is the state the row
// must be in — one of several — and HeldBy, when set, the run it must be
// held by. Actor is the run making the change; it is written into the
// worker-run column, which is how the read-back tells the winner. The
// optional fields are what else the change writes.
type Transition struct {
	ID            string
	From          []string
	HeldBy        string
	To            string
	Actor         string
	RunAfter      *time.Time
	FinishedAt    *time.Time
	LastError     *string
	ResetAttempts bool
}

func (inst Transition) storeTransition() (t watchbillstore.Transition) {
	t = watchbillstore.Transition{
		ID: inst.ID, From: inst.From, WorkerRun: inst.HeldBy, To: inst.To,
		SetWorkerRun: true, NewWorkerRun: inst.Actor,
		RunAfter: inst.RunAfter, FinishedAt: inst.FinishedAt, LastError: inst.LastError,
		ResetAttempts: inst.ResetAttempts,
	}
	return
}

// heldStates are the states in which a run holds a job.
var heldStates = []string{watchbillstore.StateRunning, watchbillstore.StateCancel}

// SqlStore is [StoreI] over the generated stores and the executor they
// were opened on. The generated stores are single-goroutine, so every
// method takes one mutex; the executor's statements run inside it too, so
// a read-back sees its own update.
type SqlStore struct {
	mu     sync.Mutex
	exec   recordstore.ExecutorI
	layout watchbillstore.Layout
	st     watchbillstore.Stores
}

var _ StoreI = (*SqlStore)(nil)

// NewSqlStore opens the stores over exec in layout. The caller provisions
// first ([watchbillstore.ProvisionIn]) and closes afterwards.
func NewSqlStore(exec recordstore.ExecutorI, layout watchbillstore.Layout) (inst *SqlStore) {
	inst = &SqlStore{exec: exec, layout: layout, st: watchbillstore.NewStores(exec, layout)}
	return
}

// Close releases the generated stores.
func (inst *SqlStore) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.st.Close()
}

func (inst *SqlStore) Enqueue(ctx context.Context, job watchbillstore.Job) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.st.Job.Begin(job.ID, time.Now().UTC()).AddJob(job).Commit(); err != nil {
		return eb.Build().Str("id", job.ID).Errorf("buffer job: %w", err)
	}
	if _, err = inst.st.Job.Flush(ctx); err != nil {
		return eh.Errorf("flush job: %w", err)
	}
	return
}

func (inst *SqlStore) Queue(ctx context.Context, kinds []string, now time.Time, limit int) (ids []string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for rec, qerr := range inst.exec.QueryArrow(ctx, watchbillstore.QueueSQL(inst.layout, kinds, now, limit)) {
		if qerr != nil {
			return nil, eh.Errorf("queue read: %w", qerr)
		}
		col := rec.Column(0)
		for i := 0; i < int(rec.NumRows()); i++ {
			ids = append(ids, col.ValueStr(i))
		}
	}
	return
}

func (inst *SqlStore) Claim(ctx context.Context, id string, workerRun string, now time.Time) (job watchbillstore.Job, won bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.exec.Exec(ctx, watchbillstore.ClaimSQL(inst.layout, id, workerRun, now)); err != nil {
		return job, false, eb.Build().Str("id", id).Errorf("claim: %w", err)
	}
	job, found, err := inst.getLocked(ctx, id)
	if err != nil || !found {
		return job, false, err
	}
	won = job.State == watchbillstore.StateRunning && job.WorkerRun == workerRun
	return
}

func (inst *SqlStore) Get(ctx context.Context, id string) (job watchbillstore.Job, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.getLocked(ctx, id)
}

func (inst *SqlStore) getLocked(ctx context.Context, id string) (job watchbillstore.Job, found bool, err error) {
	for ent, serr := range inst.st.Job.ScanJob(ctx, recordstore.ScanOpts{ExtraPredicate: watchbillstore.ReadJobPredicate(id)}) {
		if serr != nil {
			return job, false, eb.Build().Str("id", id).Errorf("read job: %w", serr)
		}
		if ent != nil && ent.Job.Has {
			job, found = ent.Job.Val, true
		}
	}
	return
}

func (inst *SqlStore) Transition(ctx context.Context, t Transition) (job watchbillstore.Job, ok bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.exec.Exec(ctx, watchbillstore.TransitionSQL(inst.layout, t.storeTransition())); err != nil {
		return job, false, eb.Build().Str("id", t.ID).Str("to", t.To).Errorf("transition: %w", err)
	}
	job, found, err := inst.getLocked(ctx, t.ID)
	if err != nil || !found {
		return job, false, err
	}
	ok = job.State == t.To && job.WorkerRun == t.Actor
	return
}

func (inst *SqlStore) Held(ctx context.Context, workerRun string) (jobs []watchbillstore.Job, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	pred := watchbillstore.HeldPredicate(heldStates, workerRun)
	for ent, serr := range inst.st.Job.ScanJob(ctx, recordstore.ScanOpts{ExtraPredicate: pred}) {
		if serr != nil {
			return nil, eh.Errorf("read held jobs: %w", serr)
		}
		if ent != nil && ent.Job.Has {
			jobs = append(jobs, ent.Job.Val)
		}
	}
	return
}

func (inst *SqlStore) WriteEvent(ctx context.Context, at time.Time, ev watchbillstore.Event) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.st.Event.Begin(ev.ID, at.UTC()).AddEvent(ev).Commit(); err != nil {
		return eb.Build().Str("id", ev.ID).Errorf("buffer event: %w", err)
	}
	if _, err = inst.st.Event.Flush(ctx); err != nil {
		return eh.Errorf("flush event: %w", err)
	}
	return
}

func (inst *SqlStore) Expire(ctx context.Context, cutoff time.Time) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.exec.Exec(ctx, watchbillstore.ExpireSQL(inst.layout, cutoff)); err != nil {
		return eh.Errorf("expire: %w", err)
	}
	return
}
