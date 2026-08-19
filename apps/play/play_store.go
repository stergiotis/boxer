package play

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

type HistoryEntry struct {
	SQL      string
	Executed time.Time
	Elapsed  time.Duration
	NumRows  int64
	// SigParams snapshots the URL-keyed signal values the run shipped
	// (ADR-0097 slice-5 D4): the run's true inputs are the buffer (with its
	// SET-bound constants) PLUS these. Restoring a history entry seeds the
	// signal store from this map. nil when the run read no signals.
	SigParams map[string]string
	ErrorText string
	// Buffer is the editor buffer the run came from, when that is not the SQL
	// that executed — a multi-statement buffer ships only the statement under
	// the caret (ADR-0130 L3). Restoring the entry restores this, so the
	// siblings come back with it. Empty means SQL is the whole buffer.
	Buffer string
}

type QueryStore struct {
	client *Client
	alloc  memory.Allocator

	mu       sync.RWMutex
	record   arrow.RecordBatch
	schema   *arrow.Schema
	numRows  int64
	err      error
	elapsed  time.Duration
	summary  Summary
	executed time.Time
	// loading mirrors isLoading but lives under mu, so Snapshot hands back a
	// (loading, executed) pair that is always mutually consistent: a reader
	// can never see loading=false against a pre-finish snapshot (executed not
	// yet advanced), which is the torn read that used to manufacture a
	// spurious idle in the query FSM. isLoading (atomic, lock-free) stays for
	// callers where a momentary skew is harmless — the Run guard, the
	// autoshot gate, the results-loading spinners.
	loading bool
	// executedSQL is the SQL text of the run that produced the current record —
	// set by finish alongside it, so SQL() and Snapshot() name the same run.
	executedSQL string
	// sourceBuffer is the editor buffer the in-flight run came from, stashed
	// by Execute for finish to record on the history entry (ADR-0130 L3). It
	// differs from the executed SQL when a multi-statement buffer shipped only
	// the statement under the caret; empty means "the executed SQL is it".
	// Written before the run's goroutine starts and read only in finish, both
	// under mu, and one run at a time per store.
	sourceBuffer string
	history      []HistoryEntry
	maxHist      int

	// closed (under mu) marks a torn-down store: a late finish() from an
	// already-running goroutine is dropped instead of resurrecting state.
	closed bool

	// progress is the in-flight run's latest in-band tick (ADR-0115
	// plane A), written by the transport goroutine while loading and
	// meaningful only then — finish() leaves it gated behind the loading
	// flag rather than racing to clear it.
	progress      runstream.Progress
	progressFresh bool

	// terminal is how the last run ended (E3, play_runstream.go). It says
	// what err cannot: that a result which arrived intact is nonetheless a
	// prefix, because the run hit a row limit it declared on itself.
	terminal runstream.Terminal

	// opts is the store's stable query_id + replace_running_query (SD5): a
	// Run after a Cancel replaces the maybe-still-running predecessor
	// server-side (ClickHouse does not kill read-only HTTP queries on
	// connection close by default).
	opts *ExecOptions

	isLoading atomic.Bool
	cancel    context.CancelFunc
	cancelMu  sync.Mutex
}

// NewQueryStore builds a store; label names its lane in server-side
// observability (system.processes / query_log) via the stable query_id.
func NewQueryStore(client *Client, alloc memory.Allocator, maxHistory int, label string) *QueryStore {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &QueryStore{client: client, alloc: alloc, maxHist: maxHistory, opts: newExecOptions(label)}
}

func (inst *QueryStore) IsLoading() bool { return inst.isLoading.Load() }

// Snapshot returns a retained view of the last result. Caller MUST call
// rec.Release() when done (nil-safe). Retaining under the read lock ensures
// a concurrent Execute→finish can't pull the record out from under us.
// executed is the time the most recent finish() completed — use it as an
// identity token for the current dataset (changes ⇒ new query). loading is
// read under the same lock as executed, so the pair is consistent: feed this
// loading to the FSM mirror rather than a separate IsLoading() call, which
// could observe the post-finish flag against this pre-finish snapshot.
func (inst *QueryStore) Snapshot() (rec arrow.RecordBatch, schema *arrow.Schema, numRows int64, loading bool, elapsed time.Duration, summary Summary, executed time.Time, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if inst.record != nil {
		inst.record.Retain()
	}
	return inst.record, inst.schema, inst.numRows, inst.loading, inst.elapsed, inst.summary, inst.executed, inst.err
}

// SQL returns the SQL text of the run that produced the current Snapshot
// result, or "" before the first run finishes. Guarded by the same lock as
// Snapshot; as a separate call it can race a concurrent finish (see PlayApp.MainSQL).
func (inst *QueryStore) SQL() string {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.executedSQL
}

// Truncation reports the reason the last result is a prefix, or "" when it
// is whole (or when the run failed, which err already says). Read it beside
// Snapshot: a capped result looks exactly like a complete one otherwise,
// which is the confusion R9 exists to prevent.
func (inst *QueryStore) Truncation() (reason string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if inst.terminal.State == runstream.TerminalTruncated {
		reason = inst.terminal.Reason
	}
	return
}

// Progress returns the in-flight run's latest in-band progress tick;
// fresh is false when no run is loading or no tick has arrived yet.
func (inst *QueryStore) Progress() (p runstream.Progress, fresh bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if inst.loading && inst.progressFresh {
		return inst.progress, true
	}
	return
}

func (inst *QueryStore) History() []HistoryEntry {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	out := make([]HistoryEntry, len(inst.history))
	copy(out, inst.history)
	return out
}

// Execute kicks off an async query. signals carries the URL-keyed signal
// values resolved for this run (ADR-0097 slice 5a; nil = none) — they ride
// the request URL and are snapshotted into the history entry (D4). Subsequent
// calls while a query is running are ignored; call Cancel first.
//
// sourceBuffer is the editor buffer sql was derived from, recorded on the
// history entry so a restore brings back the whole buffer rather than the
// fragment that ran (ADR-0130 L3 run-under-cursor). Pass "" when sql IS the
// buffer — every lane but `main` does.
func (inst *QueryStore) Execute(sql string, signals map[string]string, sourceBuffer string) {
	if inst.isLoading.Swap(true) {
		return
	}
	inst.mu.Lock()
	inst.sourceBuffer = sourceBuffer
	inst.loading = true
	inst.progress = runstream.Progress{}
	inst.progressFresh = false
	inst.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	inst.cancelMu.Lock()
	inst.cancel = cancel
	inst.cancelMu.Unlock()

	// Snapshot the caller's map: it must not mutate under the async run or
	// the history entry.
	var sigs map[string]string
	if len(signals) > 0 {
		sigs = make(map[string]string, len(signals))
		maps.Copy(sigs, signals)
	}

	go func() {
		defer cancel() // release the context (and its resources) on every path
		defer inst.isLoading.Store(false)
		defer func() {
			inst.cancelMu.Lock()
			inst.cancel = nil
			inst.cancelMu.Unlock()
		}()

		// Per-run options copy carrying the live-progress sink (ADR-0115
		// plane A): ticks land under mu while this run is loading; the
		// isLoading gate above means no second run can be in flight, so
		// no generation counter is needed here.
		opts := *inst.opts
		opts.OnProgress = func(p runstream.Progress) {
			inst.mu.Lock()
			if inst.loading && !inst.closed {
				inst.progress = p
				inst.progressFresh = true
			}
			inst.mu.Unlock()
		}

		// One resolution per run (play_dispatch.go). Taken on this goroutine,
		// not on the render thread, because it runs the client-side rewrites.
		dec := inst.client.Dispatch(sql, "")

		start := time.Now()
		rdr, rs, summary, err := inst.client.ExecuteArrowStream(ctx, sql, inst.alloc, &opts, sigs, dec)
		if err != nil {
			inst.finish(sql, sigs, start, nil, nil, 0, summary, err, runstream.Failed(err))
			return
		}
		defer func() {
			rdr.Release()
			_ = rs.Close()
		}()

		// Consume all batches and concatenate into a single record batch so
		// the renderer sees one continuous column per field. The drain runs
		// through the runstream collector (play_runstream.go), so a stream
		// that dies part-way is a failed terminal rather than a short
		// result nobody flagged, and a run capped against its own declared
		// row limit comes back marked.
		batches, term, e := drainRun(rdr, rs)
		if e != nil {
			inst.finish(sql, sigs, start, nil, nil, 0, summary, e, runstream.Failed(e))
			return
		}

		rec, schema, cErr := concatBatches(batches, inst.alloc)
		for _, b := range batches {
			b.Release()
		}
		if cErr != nil {
			inst.finish(sql, sigs, start, nil, nil, 0, summary, cErr, runstream.Failed(cErr))
			return
		}
		if schema == nil {
			// Zero batches: keep the stream schema so an empty result still
			// carries its column shape (review finding).
			schema = rdr.Schema()
		}
		var rows int64
		if rec != nil {
			rows = rec.NumRows()
		}
		inst.finish(sql, sigs, start, rec, schema, rows, summary, nil, term)
	}()
}

// Cancel aborts the in-flight query (if any).
func (inst *QueryStore) Cancel() {
	inst.cancelMu.Lock()
	c := inst.cancel
	inst.cancelMu.Unlock()
	if c != nil {
		c()
	}
}

// Close cancels any in-flight query, releases the held result, and marks the
// store closed so a late finish() is dropped rather than resurrecting state.
// Idempotent; the store is unusable afterwards.
func (inst *QueryStore) Close() {
	inst.Cancel()
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.closed = true
	if inst.record != nil {
		inst.record.Release()
		inst.record = nil
	}
	inst.schema = nil
}

func (inst *QueryStore) finish(sql string, sigs map[string]string, start time.Time, rec arrow.RecordBatch, schema *arrow.Schema, rows int64, summary Summary, err error, term runstream.Terminal) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		// Torn down while this run was in flight: drop the late result.
		if rec != nil {
			rec.Release()
		}
		return
	}
	if inst.record != nil {
		inst.record.Release()
	}
	inst.record = rec
	inst.schema = schema
	inst.numRows = rows
	inst.summary = summary
	inst.elapsed = time.Since(start)
	inst.err = err
	inst.terminal = term
	inst.executed = time.Now()
	inst.executedSQL = sql
	inst.loading = false

	entry := HistoryEntry{
		SQL:       sql,
		Executed:  inst.executed,
		Elapsed:   inst.elapsed,
		NumRows:   rows,
		SigParams: sigs,
		Buffer:    inst.sourceBuffer,
	}
	if err != nil {
		entry.ErrorText = err.Error()
		log.Warn().Err(err).Msg("query failed")
	}
	inst.history = append(inst.history, entry)
	if len(inst.history) > inst.maxHist {
		inst.history = inst.history[len(inst.history)-inst.maxHist:]
	}
}
