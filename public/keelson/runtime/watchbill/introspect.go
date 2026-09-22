package watchbill

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The table names, as keelson() resolves them and as a reader's
// `keelson.query.<table>` grant names them (ADR-0253 §SD1).
const (
	// TableJobs is the job table, one row per job.
	TableJobs = "watchbill"
	// TableEvent is the trail, one row per transition.
	TableEvent = "watchbill_event"
	// TableWorker is the cell's workers: presence rows joined with
	// liveness, this process's own with its live fields.
	TableWorker = "watchbill_worker"
)

// ListerI is the read side the introspection tables need (ADR-0223
// §SD7): every job row, and the newest event rows.
type ListerI interface {
	ListJobs(ctx context.Context, limit int) (jobs []watchbillstore.Job, err error)
	ListEvents(ctx context.Context, limit int) (events []EventRow, err error)
}

// EventRow is one event with its instant.
type EventRow struct {
	At    time.Time
	Event watchbillstore.Event
}

// snapshotLimit bounds what one introspection query reads; the tables are
// a window on the queue, not an export of it.
const snapshotLimit = 10_000

// StatusI is the process's own worker, or nil for none.
type StatusI interface {
	Status() (s Status)
}

// IntrospectDeps is what the three tables read: the job table, the
// process's worker, and the cell's presence rows with the liveness that
// says which runs are alive (ADR-0237 §SD4). Every field may be nil; the
// tables then answer with what the rest can say.
type IntrospectDeps struct {
	Lister   ListerI
	Status   StatusI
	Presence PresenceReaderI
	Liveness LivenessI
	// AbandonAfter is the sweep's rule for a dead run; zero takes the
	// environment's.
	AbandonAfter time.Duration
}

// presenceWindow bounds how far back the worker table reads presence
// rows: a run older than this that is still alive has written a started
// row more recently, since a worker declares itself at every start.
const presenceWindow = 24 * time.Hour

// RegisterIntrospect registers keelson('watchbill'),
// keelson('watchbill_event') and keelson('watchbill_worker') over deps;
// nil answers with empty tables, so the names exist whether or not a
// store or a worker was wired.
func RegisterIntrospect(r *introspect.Registry, deps IntrospectDeps) (err error) {
	if err = r.Register(jobsProvider{lister: deps.Lister}); err != nil {
		return eh.Errorf("register watchbill: %w", err)
	}
	if err = r.Register(eventsProvider{lister: deps.Lister}); err != nil {
		return eh.Errorf("register watchbill_event: %w", err)
	}
	if err = r.Register(workerProvider{deps: deps}); err != nil {
		return eh.Errorf("register watchbill_worker: %w", err)
	}
	return
}

type workerProvider struct{ deps IntrospectDeps }

func (workerProvider) Name() string                         { return TableWorker }
func (workerProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (workerProvider) Schema() *arrow.Schema                { return workerTable(nil).Schema() }

func (p workerProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	rows, err := p.rows(context.Background(), time.Now())
	if err != nil {
		return
	}
	rec = workerTable(rows).Build(proj, len(rows))
	return
}

// workerRow is one line of the table: a run on the cell as presence and
// liveness see it, with the live fields filled for this process's own
// worker.
type workerRow struct {
	WorkerInfo
	Local  bool
	Status Status
}

// rows is the cell's runs from presence, the process's own worker merged
// in — or standing alone where presence is not wired.
func (p workerProvider) rows(ctx context.Context, now time.Time) (rows []workerRow, err error) {
	var local *Status
	if p.deps.Status != nil {
		s := p.deps.Status.Status()
		local = &s
	}
	if p.deps.Presence != nil {
		found, lerr := p.deps.Presence.ListPresence(ctx, now.Add(-presenceWindow))
		if lerr != nil {
			return nil, lerr
		}
		abandonAfter := p.deps.AbandonAfter
		if abandonAfter <= 0 {
			abandonAfter = AbandonAfter.Get()
		}
		workers, ferr := FoldWorkers(ctx, found, now, abandonAfter, p.deps.Liveness)
		if ferr != nil {
			return nil, ferr
		}
		for _, w := range workers {
			row := workerRow{WorkerInfo: w}
			if local != nil && local.RunId == w.RunId {
				row.Local, row.Status = true, *local
				local = nil
			}
			rows = append(rows, row)
		}
	}
	if local != nil {
		// The process's worker declared nothing, or its row is outside
		// the window: it is here all the same, alive by construction.
		rows = append(rows, workerRow{
			WorkerInfo: WorkerInfo{RunId: local.RunId, Host: hostname(), Kinds: local.Kinds, Queues: local.Queues, MaxWorkers: uint32(max(local.MaxWorkers, 0)), Alive: true},
			Local:      true, Status: *local,
		})
	}
	return
}

// workerTable is one row per run seen on the cell (ADR-0237 §SD4): what
// it drains, whether it is alive, and — for this process's own worker —
// what it holds and when it last polled.
func workerTable(rows []workerRow) *introspect.Table {
	return introspect.NewTable().
		String("run_id", func(i int) string { return rows[i].RunId }).
		String("host", func(i int) string { return rows[i].Host }).
		StringList("kinds", func(i int) []string { return rows[i].Kinds }).
		StringList("queues", func(i int) []string { return rows[i].Queues }).
		Int64("max_workers", func(i int) int64 { return int64(rows[i].MaxWorkers) }).
		String("started_at", func(i int) string { return rfc(rows[i].StartedAt) }).
		String("stopped_at", func(i int) string { return rfc(rows[i].StoppedAt) }).
		Bool("alive", func(i int) bool { return rows[i].Alive }).
		Bool("local", func(i int) bool { return rows[i].Local }).
		StringList("running", func(i int) []string { return rows[i].Status.Running }).
		String("last_tick", func(i int) string { return rfc(rows[i].Status.LastTick) }).
		Int64("ticks", func(i int) int64 { return int64(rows[i].Status.Ticks) }).
		Int64("poll_ms", func(i int) int64 { return rows[i].Status.Poll.Milliseconds() }).
		Int64("abandon_after_ms", func(i int) int64 { return rows[i].Status.AbandonAfter.Milliseconds() }).
		Int64("keep_ms", func(i int) int64 { return rows[i].Status.Keep.Milliseconds() }).
		Bool("serving", func(i int) bool { return rows[i].Status.Serving }).
		Bool("sweeping", func(i int) bool { return rows[i].Status.Sweeping })
}

type jobsProvider struct{ lister ListerI }

func (jobsProvider) Name() string                         { return TableJobs }
func (jobsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (jobsProvider) Schema() *arrow.Schema                { return jobsTable(nil).Schema() }

func (p jobsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []watchbillstore.Job
	if p.lister != nil {
		if rows, err = p.lister.ListJobs(context.Background(), snapshotLimit); err != nil {
			return
		}
	}
	rec = jobsTable(rows).Build(proj, len(rows))
	return
}

func rfc(t time.Time) (s string) {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// jobsTable is the job row as a table: the request and its state, the
// args reported by kind and size rather than bytes.
func jobsTable(rows []watchbillstore.Job) *introspect.Table {
	return introspect.NewTable().
		String("id", func(i int) string { return rows[i].ID }).
		String("kind", func(i int) string { return rows[i].Kind }).
		String("subject", func(i int) string { return rows[i].Subject }).
		String("queue", func(i int) string { return rows[i].Queue }).
		Int64("priority", func(i int) int64 { return int64(rows[i].Priority) }).
		String("state", func(i int) string { return rows[i].State }).
		Int64("attempt", func(i int) int64 { return int64(rows[i].Attempt) }).
		Int64("max_attempts", func(i int) int64 { return int64(rows[i].MaxAttempts) }).
		String("backoff", func(i int) string { return rows[i].Backoff }).
		Int64("backoff_base_ms", func(i int) int64 { return int64(rows[i].BackoffBaseMs) }).
		Int64("timeout_ms", func(i int) int64 { return int64(rows[i].TimeoutMs) }).
		String("run_after", func(i int) string { return rfc(rows[i].RunAfter) }).
		String("worker_run", func(i int) string { return rows[i].WorkerRun }).
		String("finished_at", func(i int) string { return rfc(rows[i].FinishedAt) }).
		String("last_error", func(i int) string { return rows[i].LastError }).
		String("owner_app_id", func(i int) string { return rows[i].OwnerAppId }).
		String("requester_run", func(i int) string { return rows[i].RequesterRun }).
		String("args_kind", func(i int) string { return rows[i].ArgsKind }).
		Int64("args_bytes", func(i int) int64 { return int64(len(rows[i].Args)) })
}

type eventsProvider struct{ lister ListerI }

func (eventsProvider) Name() string                         { return TableEvent }
func (eventsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (eventsProvider) Schema() *arrow.Schema                { return eventsTable(nil).Schema() }

func (p eventsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []EventRow
	if p.lister != nil {
		if rows, err = p.lister.ListEvents(context.Background(), snapshotLimit); err != nil {
			return
		}
	}
	rec = eventsTable(rows).Build(proj, len(rows))
	return
}

func eventsTable(rows []EventRow) *introspect.Table {
	return introspect.NewTable().
		String("job_id", func(i int) string { return rows[i].Event.ID }).
		String("at", func(i int) string { return rfc(rows[i].At) }).
		String("state", func(i int) string { return rows[i].Event.State }).
		Int64("attempt", func(i int) int64 { return int64(rows[i].Event.Attempt) }).
		String("worker_run", func(i int) string { return rows[i].Event.WorkerRun }).
		String("note", func(i int) string { return rows[i].Event.Note }).
		String("error", func(i int) string { return string(rows[i].Event.Error) })
}

// ListJobs reads every job row, newest request first, up to limit.
func (inst *SqlStore) ListJobs(ctx context.Context, limit int) (jobs []watchbillstore.Job, err error) {
	return inst.List(ctx, watchbillstore.ListFilter{}, limit)
}

// ListEvents reads event rows in write order, up to limit.
func (inst *SqlStore) ListEvents(ctx context.Context, limit int) (events []EventRow, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for ent, serr := range inst.st.Event.ScanEvent(ctx, recordstore.ScanOpts{Limit: limit}) {
		if serr != nil {
			return nil, eh.Errorf("list events: %w", serr)
		}
		if ent != nil && ent.Event.Has {
			events = append(events, EventRow{At: ent.Ts, Event: ent.Event.Val})
		}
	}
	return
}

var _ ListerI = (*SqlStore)(nil)

// ListJobs is [ListerI] over the in-memory rows.
func (inst *MemStore) ListJobs(_ context.Context, limit int) (jobs []watchbillstore.Job, err error) {
	jobs = inst.Jobs()
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return
}

// ListEvents is [ListerI] over the in-memory rows.
func (inst *MemStore) ListEvents(_ context.Context, limit int) (events []EventRow, err error) {
	for _, e := range inst.Events("") {
		if limit > 0 && len(events) >= limit {
			break
		}
		events = append(events, EventRow{At: e.At, Event: e.Event})
	}
	return
}

var _ ListerI = (*MemStore)(nil)
