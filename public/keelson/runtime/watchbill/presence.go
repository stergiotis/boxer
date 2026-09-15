package watchbill

import (
	"context"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillpresence"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// A worker says what it drains as an appended fact (ADR-0237): one
// `boxer.facts` row when it starts and one when it stops cleanly, and the
// runtime heartbeat says whether a started run is still alive. Nothing is
// updated in place, so none of the claim's conditions apply here.

// PresenceI is the writer the worker calls at its edges.
type PresenceI interface {
	Started(ctx context.Context, s Status) (err error)
	Stopped(ctx context.Context, s Status) (err error)
}

// PresenceReaderI lists the rows written since an instant, newest first
// per run is not required — [FoldWorkers] orders them.
type PresenceReaderI interface {
	ListPresence(ctx context.Context, since time.Time) (rows []watchbillpresence.WorkerPresence, err error)
}

// WorkerInfo is one run on the cell as the reader folds it (ADR-0237 §SD3).
type WorkerInfo struct {
	RunId      string
	Host       string
	Kinds      []string
	Queues     []string
	MaxWorkers uint32
	StartedAt  time.Time
	// StoppedAt is zero while the run has not written its stopped row.
	StoppedAt time.Time
	// Alive is true for an unstopped run whose heartbeat is fresh; with no
	// liveness source to ask, an unstopped run is taken as alive.
	Alive bool
}

// kindLabel is the presence row's kind label.
const kindLabel = "watchbillWorker"

// Presence is [PresenceI] and [PresenceReaderI] over the generated store on
// boxer.facts. The generated store is single-goroutine; one mutex confines
// it.
type Presence struct {
	mu   sync.Mutex
	st   *watchbillpresence.PresenceStore
	host string
}

var (
	_ PresenceI       = (*Presence)(nil)
	_ PresenceReaderI = (*Presence)(nil)
)

// NewPresence opens the store over exec, which must reach the server that
// holds boxer.facts; chstore has provisioned the table. The caller closes.
func NewPresence(exec recordstore.ExecutorI) (inst *Presence) {
	inst = &Presence{st: watchbillpresence.NewPresenceStore(exec, nil, watchbillpresence.PresenceStoreConfig{}), host: hostname()}
	return
}

// Close releases the store.
func (inst *Presence) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.st.Close()
}

func (inst *Presence) Started(ctx context.Context, s Status) (err error) {
	return inst.write(ctx, watchbillpresence.PhaseStarted, s)
}

func (inst *Presence) Stopped(ctx context.Context, s Status) (err error) {
	return inst.write(ctx, watchbillpresence.PhaseStopped, s)
}

func (inst *Presence) write(ctx context.Context, phase string, s Status) (err error) {
	if s.RunId == "" {
		return eh.Errorf("watchbill: presence without a run id")
	}
	row := PresenceRow(phase, s, inst.host, time.Now())
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = inst.st.Begin(row.Id, row.Ts, watchbillpresence.PresenceEnvelope{NaturalKey: row.NaturalKey}).AddWorkerPresence(row).Commit(); err != nil {
		return eb.Build().Str("runId", s.RunId).Str("phase", phase).Errorf("buffer presence: %w", err)
	}
	if _, err = inst.st.Flush(ctx); err != nil {
		return eb.Build().Str("runId", s.RunId).Str("phase", phase).Errorf("flush presence: %w", err)
	}
	return
}

// PresenceRow is the row a worker writes: keyed by the run id so a run's
// rows form one series, the natural key the run id itself.
func PresenceRow(phase string, s Status, host string, at time.Time) (row watchbillpresence.WorkerPresence) {
	row = watchbillpresence.WorkerPresence{
		Id: xxh3.HashString(s.RunId), NaturalKey: []byte(s.RunId), Ts: at.UTC(),
		Kind: kindLabel, RunId: s.RunId, Host: host, Phase: phase,
		Kinds: append([]string(nil), s.Kinds...), Queues: append([]string(nil), s.Queues...),
		MaxWorkers: uint32(max(s.MaxWorkers, 0)),
	}
	return
}

func (inst *Presence) ListPresence(ctx context.Context, since time.Time) (rows []watchbillpresence.WorkerPresence, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	opts := recordstore.ScanOpts{ExtraPredicate: watchbillpresence.PresenceColOrder + " >= fromUnixTimestamp64Nano(" + strconv.FormatInt(since.UTC().UnixNano(), 10) + ")"}
	for ent, serr := range inst.st.ScanWorkerPresence(ctx, opts) {
		if serr != nil {
			return nil, eh.Errorf("list presence: %w", serr)
		}
		if ent != nil && ent.WorkerPresence.Has {
			rows = append(rows, ent.WorkerPresence.Val)
		}
	}
	return
}

// FoldWorkers folds presence rows to one [WorkerInfo] per run: the latest
// started row is the declaration, a later stopped row ends it, and an
// unstopped run is alive when its heartbeat is fresher than abandonAfter
// — the sweep's own rule (ADR-0223 §SD4), so the two agree about who is
// dead. Sorted by run id.
func FoldWorkers(ctx context.Context, rows []watchbillpresence.WorkerPresence, now time.Time, abandonAfter time.Duration, live LivenessI) (workers []WorkerInfo, err error) {
	byRun := make(map[string]*WorkerInfo, 8)
	for _, r := range rows {
		w, ok := byRun[r.RunId]
		if !ok {
			w = &WorkerInfo{RunId: r.RunId}
			byRun[r.RunId] = w
		}
		switch r.Phase {
		case watchbillpresence.PhaseStarted:
			if r.Ts.After(w.StartedAt) {
				w.Host, w.Kinds, w.Queues, w.MaxWorkers, w.StartedAt = r.Host, r.Kinds, r.Queues, r.MaxWorkers, r.Ts
			}
		case watchbillpresence.PhaseStopped:
			if r.Ts.After(w.StoppedAt) {
				w.StoppedAt = r.Ts
			}
		}
	}
	since := now.Add(-abandonAfter)
	for _, w := range byRun {
		if w.StartedAt.IsZero() {
			// A stopped row without its started row is a run whose start
			// fell outside the window; it has nothing to say.
			continue
		}
		stopped := !w.StoppedAt.IsZero() && !w.StoppedAt.Before(w.StartedAt)
		if stopped {
			w.Alive = false
		} else if live == nil {
			w.Alive = true
		} else {
			if w.Alive, err = live.Alive(ctx, w.RunId, since); err != nil {
				return nil, eb.Build().Str("runId", w.RunId).Errorf("presence liveness: %w", err)
			}
		}
		if !stopped {
			w.StoppedAt = time.Time{}
		}
		workers = append(workers, *w)
	}
	sort.Slice(workers, func(a, b int) bool { return workers[a].RunId < workers[b].RunId })
	return
}

func hostname() (name string) {
	name, _ = os.Hostname()
	return
}

// MemPresence is the test double: rows in memory, phases recorded.
type MemPresence struct {
	mu   sync.Mutex
	Rows []watchbillpresence.WorkerPresence
	Host string
}

var (
	_ PresenceI       = (*MemPresence)(nil)
	_ PresenceReaderI = (*MemPresence)(nil)
)

func (inst *MemPresence) Started(_ context.Context, s Status) (err error) {
	return inst.add(watchbillpresence.PhaseStarted, s)
}

func (inst *MemPresence) Stopped(_ context.Context, s Status) (err error) {
	return inst.add(watchbillpresence.PhaseStopped, s)
}

func (inst *MemPresence) add(phase string, s Status) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.Rows = append(inst.Rows, PresenceRow(phase, s, inst.Host, time.Now()))
	return
}

func (inst *MemPresence) ListPresence(_ context.Context, since time.Time) (rows []watchbillpresence.WorkerPresence, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, r := range inst.Rows {
		if !r.Ts.Before(since) {
			rows = append(rows, r)
		}
	}
	return
}
