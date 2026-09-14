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

// StatusI is what keelson('watchbill_worker') reads: the process's own
// worker, or nil for none.
type StatusI interface {
	Status() (s Status)
}

// RegisterIntrospect registers keelson('watchbill'),
// keelson('watchbill_event') and keelson('watchbill_worker') over lister
// and status; nil answers with empty tables, so the names exist whether
// or not a store or a worker was wired.
func RegisterIntrospect(r *introspect.Registry, lister ListerI, status StatusI) (err error) {
	if err = r.Register(jobsProvider{lister: lister}); err != nil {
		return eh.Errorf("register watchbill: %w", err)
	}
	if err = r.Register(eventsProvider{lister: lister}); err != nil {
		return eh.Errorf("register watchbill_event: %w", err)
	}
	if err = r.Register(workerProvider{status: status}); err != nil {
		return eh.Errorf("register watchbill_worker: %w", err)
	}
	return
}

type workerProvider struct{ status StatusI }

func (workerProvider) Name() string                         { return "watchbill_worker" }
func (workerProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (workerProvider) Schema() *arrow.Schema                { return workerTable(nil).Schema() }

func (p workerProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []Status
	if p.status != nil {
		rows = []Status{p.status.Status()}
	}
	rec = workerTable(rows).Build(proj, len(rows))
	return
}

// workerTable is one row per worker in this process (ADR-0234 §SD5) —
// one, today — with what it drains and what it holds.
func workerTable(rows []Status) *introspect.Table {
	return introspect.NewTable().
		String("run_id", func(i int) string { return rows[i].RunId }).
		StringList("kinds", func(i int) []string { return rows[i].Kinds }).
		StringList("queues", func(i int) []string { return rows[i].Queues }).
		Int64("max_workers", func(i int) int64 { return int64(rows[i].MaxWorkers) }).
		StringList("running", func(i int) []string { return rows[i].Running }).
		String("last_tick", func(i int) string { return rfc(rows[i].LastTick) }).
		Int64("ticks", func(i int) int64 { return int64(rows[i].Ticks) }).
		Int64("poll_ms", func(i int) int64 { return rows[i].Poll.Milliseconds() }).
		Int64("abandon_after_ms", func(i int) int64 { return rows[i].AbandonAfter.Milliseconds() }).
		Int64("keep_ms", func(i int) int64 { return rows[i].Keep.Milliseconds() }).
		Bool("serving", func(i int) bool { return rows[i].Serving }).
		Bool("sweeping", func(i int) bool { return rows[i].Sweeping })
}

type jobsProvider struct{ lister ListerI }

func (jobsProvider) Name() string                         { return "watchbill" }
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

func (eventsProvider) Name() string                         { return "watchbill_event" }
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
