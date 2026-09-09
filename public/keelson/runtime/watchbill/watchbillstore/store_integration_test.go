//go:build integration

package watchbillstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// One job's life under clickhouse local, one statement per process: the
// claim is a conditional update, a second claim on a running row changes
// nothing, a transition guarded on another run changes nothing, the
// event rows read back in order, and the expiry deletes what finished.
// The concurrent race is the worker's integration test, against a server.
func TestStoreRoundTripUnderClickhouseLocal(t *testing.T) {
	ctx := context.Background()
	exec, err := chexec.NewLocalExecutor(filepath.Join(t.TempDir(), "chdb"), memory.NewGoAllocator())
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	layout := Layout{Database: "wbtest"}
	require.NoError(t, ProvisionIn(ctx, exec, layout))
	require.NoError(t, ProvisionIn(ctx, exec, layout), "provisioning is idempotent")
	st := NewStores(exec, layout)
	defer st.Close()

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	job := Job{
		ID: "j1", Kind: "test.kind", Subject: "s1", Queue: "default", Priority: 5, MaxAttempts: 3,
		Backoff: BackoffNone, OwnerAppId: "app", RequesterRun: "req-run",
		State: StateQueued, RunAfter: now, Attempt: 0,
	}
	require.NoError(t, st.Job.Begin(job.ID, now).AddJob(job).Commit())
	_, err = st.Job.Flush(ctx)
	require.NoError(t, err)

	ids := queueIds(t, exec, layout, now)
	require.Equal(t, []string{"j1"}, ids)
	assert.Empty(t, queueIds(t, exec, layout, now.Add(-time.Second)), "not due yet")

	// The claim, then the read-back that says who holds it.
	require.NoError(t, exec.Exec(ctx, ClaimSQL(layout, "j1", "run-a", now)))
	got := readJob(t, ctx, st, "j1")
	assert.Equal(t, StateRunning, got.State)
	assert.Equal(t, "run-a", got.WorkerRun)
	assert.EqualValues(t, 1, got.Attempt)

	// A second claimant finds the row taken and changes nothing.
	require.NoError(t, exec.Exec(ctx, ClaimSQL(layout, "j1", "run-b", now)))
	got = readJob(t, ctx, st, "j1")
	assert.Equal(t, "run-a", got.WorkerRun)
	assert.EqualValues(t, 1, got.Attempt)

	// A transition guarded on the wrong run is inert; on the right run it lands.
	fin := now.Add(time.Minute)
	require.NoError(t, exec.Exec(ctx, TransitionSQL(layout, Transition{ID: "j1", From: []string{StateRunning}, WorkerRun: "run-b", To: StateSucceeded, FinishedAt: &fin})))
	assert.Equal(t, StateRunning, readJob(t, ctx, st, "j1").State)
	require.NoError(t, exec.Exec(ctx, TransitionSQL(layout, Transition{ID: "j1", From: []string{StateRunning}, WorkerRun: "run-a", To: StateSucceeded, FinishedAt: &fin, SetWorkerRun: true})))
	got = readJob(t, ctx, st, "j1")
	assert.Equal(t, StateSucceeded, got.State)
	assert.Equal(t, "", got.WorkerRun)
	assert.Equal(t, fin, got.FinishedAt.UTC())

	// Events append and read back in order.
	for i, s := range []string{StateRunning, StateSucceeded} {
		require.NoError(t, st.Event.Begin("j1", now.Add(time.Duration(i)*time.Second)).AddEvent(Event{ID: "j1", State: s, Attempt: 1, WorkerRun: "run-a"}).Commit())
	}
	_, err = st.Event.Flush(ctx)
	require.NoError(t, err)
	var states []string
	for ent, serr := range st.Event.ScanEvent(ctx, recordstore.ScanOpts{ExtraPredicate: EventColKey + " = 'j1'"}) {
		require.NoError(t, serr)
		states = append(states, ent.Event.Val.State)
	}
	assert.Equal(t, []string{StateRunning, StateSucceeded}, states)

	// Expiry removes finished rows before the cutoff and nothing else.
	require.NoError(t, exec.Exec(ctx, ExpireSQL(layout, fin)))
	assert.NotNil(t, readJobOpt(t, ctx, st, "j1"), "finished at the cutoff is not before it")
	require.NoError(t, exec.Exec(ctx, ExpireSQL(layout, fin.Add(time.Nanosecond))))
	assert.Nil(t, readJobOpt(t, ctx, st, "j1"))
}

func queueIds(t *testing.T, exec recordstore.ExecutorI, layout Layout, now time.Time) (ids []string) {
	t.Helper()
	for rec, err := range exec.QueryArrow(context.Background(), QueueSQL(layout, nil, now, 0)) {
		require.NoError(t, err)
		c := rec.Column(0)
		for i := 0; i < int(rec.NumRows()); i++ {
			ids = append(ids, c.ValueStr(i))
		}
	}
	return
}

func readJobOpt(t *testing.T, ctx context.Context, st Stores, id string) (job *Job) {
	t.Helper()
	for ent, err := range st.Job.ScanJob(ctx, recordstore.ScanOpts{ExtraPredicate: ReadJobPredicate(id)}) {
		require.NoError(t, err)
		if ent != nil && ent.Job.Has {
			j := ent.Job.Val
			job = &j
		}
	}
	return
}

func readJob(t *testing.T, ctx context.Context, st Stores, id string) (job Job) {
	t.Helper()
	p := readJobOpt(t, ctx, st, id)
	require.NotNil(t, p, "job %s", id)
	return *p
}
