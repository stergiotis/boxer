package recordstore

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/identity/callident"
)

// fakeExec is an ExecutorI that records what reached it: the statements, and
// the batch id each call's context carried.
type fakeExec struct {
	alloc       memory.Allocator
	batches     int
	rowsPer     int
	err         error // returned by Exec and InsertArrow
	failAtBatch int   // a query yields (nil, err) in place of this batch; 0 never
	mu          sync.Mutex
	calls       int
	batchIds    []BatchIdT
}

func (inst *fakeExec) record(ctx context.Context) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.calls++
	id, _ := BatchIdFrom(ctx)
	inst.batchIds = append(inst.batchIds, id)
}

func (inst *fakeExec) Exec(ctx context.Context, _ string) error {
	inst.record(ctx)
	return inst.err
}

func (inst *fakeExec) InsertArrow(ctx context.Context, _ string, _ []arrow.RecordBatch) error {
	inst.record(ctx)
	return inst.err
}

func (inst *fakeExec) QueryArrow(ctx context.Context, _ string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		inst.record(ctx)
		for i := range inst.batches {
			if inst.failAtBatch == i+1 {
				yield(nil, errors.New("Code: 241. DB::Exception: Memory limit exceeded"))
				return
			}
			if !yield(inst.batch(), nil) {
				return
			}
		}
	}
}

func (inst *fakeExec) batch() arrow.RecordBatch {
	b := array.NewInt64Builder(inst.alloc)
	defer b.Release()
	for i := range inst.rowsPer {
		b.Append(int64(i))
	}
	col := b.NewArray()
	defer col.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	return array.NewRecordBatch(schema, []arrow.Array{col}, int64(inst.rowsPer))
}

// sink collects done and intent events.
type sink struct {
	mu     sync.Mutex
	events []CallEvent
	err    error
}

func (inst *sink) ObserveCall(ev CallEvent) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.events = append(inst.events, ev)
	return inst.err
}

func (inst *sink) all() []CallEvent {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]CallEvent(nil), inst.events...)
}

func identityCtx() context.Context {
	return callident.WithCallIdentity(context.Background(), callident.CallIdentity{
		Origin: callident.Origin{Run: "run-1", App: "example.vault", Instance: 4},
		Claims: callident.Claims{Principal: "p:7f3a", Purpose: "dsar", Correlation: "op-9"},
	})
}

func unkeyed(sql string) (d SQLDigest) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte(sql))
	h.Sum(d[:0])
	return
}

func TestObserve_Exec(t *testing.T) {
	inner := &fakeExec{}
	s := &sink{}
	ex := ObserveExecutor(inner, s)
	const sql = "ALTER TABLE db.subjects DELETE WHERE email = 'alice@example.org'"
	require.NoError(t, ex.Exec(identityCtx(), sql))

	evs := s.all()
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, CallPhaseDone, ev.Phase)
	assert.Equal(t, CallOpExec, ev.Op)
	assert.Equal(t, StatementClassMutation, ev.Class)
	assert.Equal(t, "db.subjects", ev.Table)
	assert.Equal(t, CallOutcomeOk, ev.Outcome)
	assert.Equal(t, unkeyed(sql), ev.Digest)
	assert.Equal(t, "p:7f3a", ev.Identity.Claims.Principal)
	assert.Equal(t, "example.vault", ev.Identity.Origin.App)
	assert.False(t, ev.Start.IsZero())
	require.NotEmpty(t, ev.BatchId)
	assert.Equal(t, []BatchIdT{ev.BatchId}, inner.batchIds, "the minted batch id reaches the inner executor")
	assert.NotContains(t, fmt.Sprintf("%+v", ev), "alice", "an event never carries the statement's text or values")
}

func TestObserve_ExecError(t *testing.T) {
	inner := &fakeExec{err: errors.New("clickhouse http 404: Code: 60. DB::Exception: Unknown table")}
	s := &sink{}
	err := ObserveExecutor(inner, s).Exec(context.Background(), "DROP TABLE t")
	require.Error(t, err)
	ev := s.all()[0]
	assert.Equal(t, CallOutcomeError, ev.Outcome)
	assert.Equal(t, int32(60), ev.ErrCode)
	assert.Same(t, err, ev.Err)
	assert.Equal(t, StatementClassDDL, ev.Class)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inner.err = context.Canceled
	_ = ObserveExecutor(inner, s).Exec(ctx, "SELECT 1")
	assert.Equal(t, CallOutcomeCanceled, s.all()[1].Outcome)
}

func TestObserve_Insert_ReusesContextBatchId(t *testing.T) {
	inner := &fakeExec{alloc: memory.NewGoAllocator(), rowsPer: 3}
	s := &sink{}
	recs := []arrow.RecordBatch{inner.batch(), inner.batch()}
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	ctx := WithBatchId(identityCtx(), "B-store")
	require.NoError(t, ObserveExecutor(inner, s).InsertArrow(ctx, "db.ledger", recs))
	ev := s.all()[0]
	assert.Equal(t, CallOpInsert, ev.Op)
	assert.Equal(t, StatementClassUnspecified, ev.Class)
	assert.Equal(t, "db.ledger", ev.Table)
	assert.Equal(t, int64(6), ev.Rows)
	assert.Equal(t, int64(2), ev.Batches)
	assert.Equal(t, BatchIdT("B-store"), ev.BatchId)
	assert.Equal(t, []BatchIdT{"B-store"}, inner.batchIds)
}

func TestObserve_Query_Drained(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	inner := &fakeExec{alloc: alloc, batches: 3, rowsPer: 5}
	s := &sink{}
	seq := ObserveExecutor(inner, s).QueryArrow(identityCtx(), "SELECT v FROM db.t WHERE k = 'x'")
	assert.Empty(t, s.all(), "a sequence not yet iterated has issued nothing")
	n := 0
	for rec, err := range seq {
		require.NoError(t, err)
		n++
		rec.Release()
	}
	assert.Equal(t, 3, n)
	evs := s.all()
	require.Len(t, evs, 1)
	assert.Equal(t, CallOpQuery, evs[0].Op)
	assert.Equal(t, "db.t", evs[0].Table)
	assert.Equal(t, int64(15), evs[0].Rows)
	assert.Equal(t, int64(3), evs[0].Batches)
	assert.Equal(t, CallOutcomeOk, evs[0].Outcome)
}

func TestObserve_Query_EarlyBreak(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	inner := &fakeExec{alloc: alloc, batches: 5, rowsPer: 2}
	s := &sink{}
	n := 0
	for rec, err := range ObserveExecutor(inner, s).QueryArrow(context.Background(), "SELECT v FROM t") {
		require.NoError(t, err)
		n++
		rec.Release() // the consumer owns the batch it breaks on, too
		if n == 2 {
			break
		}
	}
	evs := s.all()
	require.Len(t, evs, 1, "the event fires once, when the stream ends")
	assert.Equal(t, CallOutcomeStopped, evs[0].Outcome)
	assert.Equal(t, int64(4), evs[0].Rows, "rows counted as the consumer drained them")
	assert.Equal(t, int64(2), evs[0].Batches)
}

func TestObserve_Query_MidStreamError(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	inner := &fakeExec{alloc: alloc, batches: 4, rowsPer: 1, failAtBatch: 3}
	s := &sink{}
	var gotErr error
	for rec, err := range ObserveExecutor(inner, s).QueryArrow(context.Background(), "SELECT 1") {
		if err != nil {
			gotErr = err
			continue
		}
		rec.Release()
	}
	require.Error(t, gotErr)
	ev := s.all()[0]
	assert.Equal(t, CallOutcomeError, ev.Outcome)
	assert.Equal(t, int32(241), ev.ErrCode)
	assert.Equal(t, int64(2), ev.Batches)
}

func TestObserve_ObserverErrorNeverFailsTheCall(t *testing.T) {
	s := &sink{err: errors.New("audit store down")}
	ex := ObserveExecutor(&fakeExec{}, s)
	require.NoError(t, ex.Exec(context.Background(), "SELECT 1"))
	assert.Equal(t, uint64(1), ex.ObserverErrors())
}

func TestObserve_Required_Refuses(t *testing.T) {
	inner := &fakeExec{}
	gate := &sink{err: errors.New("audit ledger unreachable")}
	other := &sink{}
	ex := ObserveExecutor(inner, Required(gate), other)
	err := ex.Exec(identityCtx(), "ALTER TABLE t UPDATE x = 1 WHERE k = 2")
	require.ErrorIs(t, err, ErrObserverRefused)
	assert.Zero(t, inner.calls, "a refused statement never reaches the server")

	gev := gate.all()
	require.Len(t, gev, 2, "intent, then the refusal's done event")
	assert.Equal(t, CallPhaseIntent, gev[0].Phase)
	assert.Equal(t, StatementClassMutation, gev[0].Class)
	assert.Equal(t, CallPhaseDone, gev[1].Phase)
	assert.Equal(t, CallOutcomeRefused, gev[1].Outcome)
	oev := other.all()
	require.Len(t, oev, 1, "a non-required observer sees only the done event")
	assert.Equal(t, CallOutcomeRefused, oev[0].Outcome)

	// A query refused at intent yields the refusal as its only pair.
	var qerr error
	for _, e := range ex.QueryArrow(context.Background(), "SELECT 1") {
		qerr = e
	}
	require.ErrorIs(t, qerr, ErrObserverRefused)
	assert.Zero(t, inner.calls)
}

func TestObserve_Required_DoneFailureIsCounted(t *testing.T) {
	inner := &fakeExec{}
	var phases []CallPhaseE
	gate := ObserverFunc(func(ev CallEvent) error {
		phases = append(phases, ev.Phase)
		if ev.Phase == CallPhaseDone {
			return errors.New("lost the outcome")
		}
		return nil
	})
	ex := ObserveExecutor(inner, Required(gate))
	require.NoError(t, ex.Exec(context.Background(), "ALTER TABLE t DELETE WHERE 1"), "the effect has happened; the done failure is not returned")
	assert.Equal(t, []CallPhaseE{CallPhaseIntent, CallPhaseDone}, phases)
	assert.Equal(t, 1, inner.calls)
	assert.Equal(t, uint64(1), ex.ObserverErrors())
}

func TestObserve_RequiredAsyncPanics(t *testing.T) {
	a := NewAsyncObserver(nil, 1)
	defer a.Close()
	assert.Panics(t, func() { ObserveExecutor(&fakeExec{}, Required(a)) })
}

func TestObserve_DigestKey(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 1
	s := &sink{}
	require.NoError(t, ObserveExecutorWith(&fakeExec{}, ObserveOptions{DigestKey: key}, s).Exec(context.Background(), "SELECT 1"))
	assert.NotEqual(t, unkeyed("SELECT 1"), s.all()[0].Digest)
	assert.Panics(t, func() { ObserveExecutorWith(&fakeExec{}, ObserveOptions{DigestKey: []byte("short")}) })
}

func TestAsyncObserver_DropsAndCounts(t *testing.T) {
	release := make(chan struct{})
	got := make(chan CallEvent, 8)
	inner := ObserverFunc(func(ev CallEvent) error {
		got <- ev
		<-release
		return nil
	})
	a := NewAsyncObserver(inner, 1)
	require.NoError(t, a.ObserveCall(CallEvent{Table: "e1"}))
	select {
	case ev := <-got: // the forwarder holds e1, blocked in the inner observer
		assert.Equal(t, "e1", ev.Table)
	case <-time.After(5 * time.Second):
		t.Fatal("forwarder never picked up the first event")
	}
	start := time.Now()
	require.NoError(t, a.ObserveCall(CallEvent{Table: "e2"})) // fills the buffer
	require.NoError(t, a.ObserveCall(CallEvent{Table: "e3"})) // dropped
	require.NoError(t, a.ObserveCall(CallEvent{Table: "e4"})) // dropped
	assert.Less(t, time.Since(start), time.Second, "ObserveCall never blocks")
	assert.Equal(t, uint64(2), a.Dropped())

	close(release)
	a.Close()
	assert.Equal(t, "e2", (<-got).Table, "Close drains what was buffered")
	require.NoError(t, a.ObserveCall(CallEvent{}))
	assert.Equal(t, uint64(3), a.Dropped(), "an event after Close is dropped")
}

func TestMultiObserver_JoinsErrors(t *testing.T) {
	e1, e2 := errors.New("one"), errors.New("two")
	m := MultiObserver{&sink{err: e1}, nil, &sink{}, &sink{err: e2}}
	err := m.ObserveCall(CallEvent{})
	assert.ErrorIs(t, err, e1)
	assert.ErrorIs(t, err, e2)
}

func TestClassifyStatement(t *testing.T) {
	for _, tc := range []struct {
		sql   string
		class StatementClassE
		table string
	}{
		{"ALTER TABLE db.t DELETE WHERE id = 1", StatementClassMutation, "db.t"},
		{"alter table `db`.`t` on cluster c1 update x = 1 where id = 2", StatementClassMutation, "db.t"},
		{"ALTER TABLE t APPLY DELETED MASK", StatementClassMutation, "t"},
		{"ALTER TABLE IF EXISTS t DELETE WHERE 1", StatementClassMutation, "t"},
		{"ALTER TABLE t ADD COLUMN c UInt8", StatementClassDDL, "t"},
		{"ALTER TABLE t DROP PARTITION 202601", StatementClassDDL, "t"},
		{"ALTER TABLE t MODIFY SETTING enable_block_number_column=1", StatementClassDDL, "t"},
		{"ALTER USER bob IDENTIFIED BY 'x'", StatementClassDDL, ""},
		{"DELETE FROM db.t WHERE k = 'a' SETTINGS lightweight_delete_mode='lightweight_update'", StatementClassMutation, "db.t"},
		{"UPDATE jobs SET claim = 1 WHERE id = 3 SETTINGS update_parallel_mode='sync'", StatementClassMutation, "jobs"},
		{"  -- leading comment\n/* block */ CREATE TABLE IF NOT EXISTS db.t (k UInt64) ENGINE = MergeTree ORDER BY k", StatementClassDDL, "db.t"},
		{"CREATE DATABASE IF NOT EXISTS db", StatementClassDDL, ""},
		{"CREATE OR REPLACE VIEW v AS SELECT 1", StatementClassDDL, "v"},
		{"DROP TABLE IF EXISTS t", StatementClassDDL, "t"},
		{"TRUNCATE TABLE IF EXISTS t", StatementClassDDL, "t"},
		{"TRUNCATE t", StatementClassDDL, "t"},
		{"INSERT INTO db.t SELECT * FROM db.s", StatementClassOther, "db.t"},
		{"SELECT a FROM (SELECT 1 AS a) AS x", StatementClassOther, ""},
		{"WITH w AS (SELECT 1) SELECT * FROM db.t", StatementClassOther, "db.t"},
		{"SELECT 'DELETE FROM x'", StatementClassOther, ""},
		{"OPTIMIZE TABLE t FINAL", StatementClassOther, "t"},
		{"SYSTEM FLUSH LOGS", StatementClassOther, ""},
		{"", StatementClassOther, ""},
		{"ALTER TABLE t ON CLUSTER 'c1' DELETE WHERE k = 1", StatementClassMutation, "t"},
		{"ALTER TABLE t ON CLUSTER '{cluster}' UPDATE x = 'a,b' WHERE k = 1", StatementClassMutation, "t"},
		{"ALTER TABLE t ADD COLUMN c UInt8, DELETE WHERE k = 1", StatementClassMutation, "t"},
		{"ALTER TABLE t MODIFY COLUMN c Tuple(a UInt8, b UInt8), MODIFY TTL d + INTERVAL 1 DAY DELETE", StatementClassDDL, "t"},
		{"ALTER TABLE t ADD COLUMN c UInt8 DEFAULT 'x, DELETE'", StatementClassDDL, "t"},
		{"TRUNCATE DATABASE db", StatementClassDDL, ""},
		{"TRUNCATE ALL TABLES FROM db", StatementClassDDL, ""},
		{"EXCHANGE TABLES a AND b", StatementClassDDL, "a"},
		{"INSERT INTO FUNCTION s3('u') SELECT * FROM subjects", StatementClassOther, ""},
	} {
		class, table := ClassifyStatement(tc.sql)
		assert.Equal(t, tc.class, class, tc.sql)
		assert.Equal(t, tc.table, table, tc.sql)
	}
}

func TestLogComment(t *testing.T) {
	assert.Equal(t, "", LogComment(context.Background()), "nothing on the context, no stamp")
	ctx := WithBatchId(identityCtx(), "B1")
	st, ok := logcomment.Parse(LogComment(ctx))
	require.True(t, ok)
	assert.Equal(t, logcomment.Stamp{RunId: "run-1", App: "example.vault", Instance: 4,
		Principal: "p:7f3a", Purpose: "dsar", Correlation: "op-9", Batch: "B1"}, st)
	st, ok = logcomment.Parse(LogComment(WithBatchId(context.Background(), "B2")))
	require.True(t, ok)
	assert.Equal(t, logcomment.Stamp{Batch: "B2"}, st)
}

// A Required anywhere but directly on ObserveExecutor would gate nothing, and
// a Required over an AsyncObserver at any depth would acknowledge an intent it
// has not recorded: both are refused at construction.
func TestObserve_RequiredPlacement(t *testing.T) {
	a := NewAsyncObserver(nil, 1)
	defer a.Close()
	for name, obs := range map[string]CallObserverI{
		"required in multi":          MultiObserver{Required(&sink{}), &sink{}},
		"required in nested multi":   MultiObserver{MultiObserver{Required(&sink{})}},
		"required behind async":      NewAsyncObserver(Required(&sink{}), 1),
		"required over multi(async)": Required(MultiObserver{&sink{}, a}),
		"required over required":     Required(Required(&sink{})),
	} {
		assert.Panics(t, func() { ObserveExecutor(&fakeExec{}, obs) }, name)
	}
	// A MultiObserver under Required, holding no async observer, is fine.
	assert.NotPanics(t, func() { ObserveExecutor(&fakeExec{}, Required(MultiObserver{&sink{}, &sink{}})) })
}

// An empty insert sends no request (ExecutorI), so it emits no event.
func TestObserve_EmptyInsertEmitsNothing(t *testing.T) {
	inner := &fakeExec{}
	s := &sink{}
	require.NoError(t, ObserveExecutor(inner, s).InsertArrow(context.Background(), "t", nil))
	assert.Empty(t, s.all())
	assert.Equal(t, 1, inner.calls, "still passed through")
}

// Under ObserveCall racing Close every event is either forwarded or counted
// as dropped — none vanishes.
func TestAsyncObserver_CloseAccountsForEveryEvent(t *testing.T) {
	for range 50 {
		var forwarded atomic.Uint64
		a := NewAsyncObserver(ObserverFunc(func(CallEvent) error { forwarded.Add(1); return nil }), 8)
		const senders, each = 8, 200
		var wg sync.WaitGroup
		for range senders {
			wg.Go(func() {
				for range each {
					_ = a.ObserveCall(CallEvent{})
				}
			})
		}
		a.Close()
		wg.Wait()
		require.Equal(t, uint64(senders*each), forwarded.Load()+a.Dropped())
	}
}
