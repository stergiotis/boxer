//go:build integration

package watchbill

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// serverExec is an executor over the ClickHouse server the CLICKHOUSE_*
// variables name, or a skip. The race below needs a server: `clickhouse
// local` runs one process per statement, so it cannot race anything with
// itself, and the serialising property under test is the server's.
func serverExec(t *testing.T) (exec recordstore.ExecutorI) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	return
}

// Twenty workers over one server race one job: exactly one handler runs,
// every other claim reads another run's mark, and the row ends succeeded
// by the run that ran it (ADR-0223 §SD3, Verification plan).
func TestTwentyWorkersRaceOneJobOnTheServer(t *testing.T) {
	ctx := context.Background()
	exec := serverExec(t)
	layout := watchbillstore.Layout{Database: "wbtest"}
	require.NoError(t, exec.Exec(ctx, "DROP TABLE IF EXISTS "+layout.JobTable()))
	require.NoError(t, exec.Exec(ctx, "DROP TABLE IF EXISTS "+layout.EventTable()))
	require.NoError(t, watchbillstore.ProvisionIn(ctx, exec, layout))

	const workers = 20
	var runs atomic.Int32
	var ranBy sync.Map
	reg := NewRegistry()
	require.NoError(t, reg.Register(HandlerFunc{KindName: "race.kind", Run: func(_ context.Context, job watchbillstore.Job, _ task.HandleI) error {
		runs.Add(1)
		ranBy.Store(job.ID, job.WorkerRun)
		time.Sleep(50 * time.Millisecond)
		return nil
	}}))

	for round := 0; round < 10; round++ {
		enq := NewSqlStore(exec, layout)
		id, err := Enqueue(ctx, enq, Request{Kind: "race.kind", Subject: "the one"})
		require.NoError(t, err)
		enq.Close()
		runs.Store(0)

		var wg sync.WaitGroup
		var stores []*SqlStore
		for i := 0; i < workers; i++ {
			st := NewSqlStore(exec, layout)
			stores = append(stores, st)
			w, err := New(Config{Store: st, Handlers: reg, RunId: "run-" + string(rune('a'+i)), Poll: time.Hour, Keep: time.Hour, AbandonAfter: time.Minute, Log: zerolog.Nop()})
			require.NoError(t, err)
			wg.Add(1)
			go func() {
				defer wg.Done()
				var runsWg sync.WaitGroup
				assert.NoError(t, w.Tick(ctx, &runsWg))
				runsWg.Wait()
			}()
		}
		wg.Wait()
		for _, st := range stores {
			st.Close()
		}
		assert.EqualValues(t, 1, runs.Load(), "round %d: exactly one handler ran", round)

		reader := NewSqlStore(exec, layout)
		job, found, err := reader.Get(ctx, id)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, watchbillstore.StateSucceeded, job.State)
		assert.EqualValues(t, 1, job.Attempt)
		by, _ := ranBy.Load(id)
		assert.Equal(t, by, job.WorkerRun, "the row is settled by the run that ran it")
		events, err := reader.ListEvents(ctx, 100)
		require.NoError(t, err)
		var mine []string
		for _, e := range events {
			if e.Event.ID == id {
				mine = append(mine, e.Event.State)
			}
		}
		assert.Equal(t, []string{watchbillstore.StateRunning, watchbillstore.StateSucceeded}, mine)
		reader.Close()
	}
}

// A run that stops mid-job leaves the row running; another run with a
// liveness that calls it dead sweeps it into a retry.
func TestSweepOnTheServer(t *testing.T) {
	ctx := context.Background()
	exec := serverExec(t)
	layout := watchbillstore.Layout{Database: "wbtest"}
	require.NoError(t, exec.Exec(ctx, "DROP TABLE IF EXISTS "+layout.JobTable()))
	require.NoError(t, exec.Exec(ctx, "DROP TABLE IF EXISTS "+layout.EventTable()))
	require.NoError(t, watchbillstore.ProvisionIn(ctx, exec, layout))
	st := NewSqlStore(exec, layout)
	defer st.Close()

	id, err := Enqueue(ctx, st, Request{Kind: "sweep.kind", MaxAttempts: 2})
	require.NoError(t, err)
	_, won, err := st.Claim(ctx, id, "run-dead", time.Now())
	require.NoError(t, err)
	require.True(t, won)

	reg := NewRegistry()
	var ran atomic.Int32
	require.NoError(t, reg.Register(HandlerFunc{KindName: "sweep.kind", Run: func(context.Context, watchbillstore.Job, task.HandleI) error { ran.Add(1); return nil }}))
	w, err := New(Config{Store: st, Handlers: reg, RunId: "run-live", Liveness: MemLiveness{Live: map[string]bool{"run-live": true}}, Poll: time.Hour, Keep: time.Hour, AbandonAfter: time.Minute, Log: zerolog.Nop()})
	require.NoError(t, err)
	var wg sync.WaitGroup
	require.NoError(t, w.Tick(ctx, &wg))
	wg.Wait()
	job, _, err := st.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, watchbillstore.StateSucceeded, job.State)
	assert.EqualValues(t, 2, job.Attempt)
	assert.EqualValues(t, 1, ran.Load())
}
