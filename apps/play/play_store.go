//go:build llm_generated_opus47

package play

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
)

type HistoryEntry struct {
	SQL       string
	Executed  time.Time
	Elapsed   time.Duration
	NumRows   int64
	ErrorText string
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
	history []HistoryEntry
	maxHist int

	isLoading atomic.Bool
	cancel    context.CancelFunc
	cancelMu  sync.Mutex
}

func NewQueryStore(client *Client, alloc memory.Allocator, maxHistory int) *QueryStore {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &QueryStore{client: client, alloc: alloc, maxHist: maxHistory}
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

func (inst *QueryStore) History() []HistoryEntry {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	out := make([]HistoryEntry, len(inst.history))
	copy(out, inst.history)
	return out
}

// Execute kicks off an async query. Subsequent calls while a query is running
// are ignored; call Cancel first.
func (inst *QueryStore) Execute(sql string) {
	if inst.isLoading.Swap(true) {
		return
	}
	inst.mu.Lock()
	inst.loading = true
	inst.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	inst.cancelMu.Lock()
	inst.cancel = cancel
	inst.cancelMu.Unlock()

	go func() {
		defer inst.isLoading.Store(false)
		defer func() {
			inst.cancelMu.Lock()
			inst.cancel = nil
			inst.cancelMu.Unlock()
		}()

		start := time.Now()
		rdr, body, summary, err := inst.client.ExecuteArrowStream(ctx, sql, inst.alloc)
		if err != nil {
			inst.finish(sql, start, nil, nil, 0, summary, err)
			return
		}
		defer func() {
			rdr.Release()
			_ = body.Close()
		}()

		// Consume all batches and concatenate into a single record batch so
		// the renderer sees one continuous column per field.
		var batches []arrow.RecordBatch
		for rdr.Next() {
			b := rdr.Record()
			b.Retain()
			batches = append(batches, b)
		}
		if e := rdr.Err(); e != nil {
			for _, b := range batches {
				b.Release()
			}
			inst.finish(sql, start, nil, nil, 0, summary, e)
			return
		}

		rec, schema, cErr := concatBatches(batches, inst.alloc)
		for _, b := range batches {
			b.Release()
		}
		if cErr != nil {
			inst.finish(sql, start, nil, nil, 0, summary, cErr)
			return
		}
		var rows int64
		if rec != nil {
			rows = rec.NumRows()
		}
		inst.finish(sql, start, rec, schema, rows, summary, nil)
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

func (inst *QueryStore) finish(sql string, start time.Time, rec arrow.RecordBatch, schema *arrow.Schema, rows int64, summary Summary, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.record != nil {
		inst.record.Release()
	}
	inst.record = rec
	inst.schema = schema
	inst.numRows = rows
	inst.summary = summary
	inst.elapsed = time.Since(start)
	inst.err = err
	inst.executed = time.Now()
	inst.loading = false

	entry := HistoryEntry{
		SQL:      sql,
		Executed: inst.executed,
		Elapsed:  inst.elapsed,
		NumRows:  rows,
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
