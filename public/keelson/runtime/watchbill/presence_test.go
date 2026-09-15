package watchbill

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillpresence"
)

// The fold: the latest started row is the declaration, a later stopped
// row ends it, an unstopped run is alive by its heartbeat, and a stopped
// row alone says nothing (ADR-0237 §SD3).
func TestFoldWorkers(t *testing.T) {
	now := t0.Add(time.Hour)
	row := func(run, phase string, at time.Time, kinds ...string) watchbillpresence.WorkerPresence {
		return PresenceRow(phase, Status{RunId: run, Kinds: kinds, Queues: []string{"bulk"}, MaxWorkers: 2}, "box-"+run, at)
	}
	rows := []watchbillpresence.WorkerPresence{
		row("a", watchbillpresence.PhaseStarted, now.Add(-30*time.Minute), "k1"),
		row("b", watchbillpresence.PhaseStarted, now.Add(-20*time.Minute), "k2"),
		row("b", watchbillpresence.PhaseStopped, now.Add(-10*time.Minute)),
		row("c", watchbillpresence.PhaseStarted, now.Add(-40*time.Minute), "k3"),
		row("c", watchbillpresence.PhaseStopped, now.Add(-35*time.Minute)),
		row("c", watchbillpresence.PhaseStarted, now.Add(-5*time.Minute), "k3", "k4"),
		row("d", watchbillpresence.PhaseStopped, now.Add(-2*time.Minute)),
		row("e", watchbillpresence.PhaseStarted, now.Add(-50*time.Minute), "k5"),
	}
	live := MemLiveness{Live: map[string]bool{"a": true, "c": true}}
	workers, err := FoldWorkers(context.Background(), rows, now, 90*time.Second, live)
	require.NoError(t, err)
	require.Len(t, workers, 4, "d has no started row in the window")
	byRun := map[string]WorkerInfo{}
	for _, w := range workers {
		byRun[w.RunId] = w
	}
	assert.True(t, byRun["a"].Alive)
	assert.Equal(t, []string{"k1"}, byRun["a"].Kinds)
	assert.Equal(t, "box-a", byRun["a"].Host)
	assert.False(t, byRun["b"].Alive, "stopped")
	assert.False(t, byRun["b"].StoppedAt.IsZero())
	assert.True(t, byRun["c"].Alive, "restarted after a stop: the latest start wins")
	assert.Equal(t, []string{"k3", "k4"}, byRun["c"].Kinds)
	assert.True(t, byRun["c"].StoppedAt.IsZero(), "the old stop is before the new start")
	assert.False(t, byRun["e"].Alive, "no heartbeat")

	noLive, err := FoldWorkers(context.Background(), rows, now, 90*time.Second, nil)
	require.NoError(t, err)
	for _, w := range noLive {
		if w.RunId == "e" {
			assert.True(t, w.Alive, "without a liveness source an unstopped run is taken as alive")
		}
	}
}

// The worker declares itself at Start and at Stop, and the worker table
// merges the cell's runs with the process's own.
func TestWorkerDeclaresPresenceAndTableMergesIt(t *testing.T) {
	pres := &MemPresence{Host: "here"}
	f := newFixture(t, "run-p", func(c *Config) { c.Presence = pres; c.Queues = []string{"bulk"} })
	require.NoError(t, f.w.Start(context.Background()))
	require.Len(t, pres.Rows, 1)
	assert.Equal(t, watchbillpresence.PhaseStarted, pres.Rows[0].Phase)
	assert.Equal(t, "run-p", pres.Rows[0].RunId)
	assert.Equal(t, []string{"test.kind"}, pres.Rows[0].Kinds)
	assert.Equal(t, []string{"bulk"}, pres.Rows[0].Queues)
	assert.Equal(t, "here", pres.Rows[0].Host)
	assert.Equal(t, "watchbillWorker", pres.Rows[0].Kind)

	// Another process on the cell, alive; and one that stopped.
	require.NoError(t, pres.Started(context.Background(), Status{RunId: "run-other", Kinds: []string{"other.kind"}, MaxWorkers: 4}))
	require.NoError(t, pres.Started(context.Background(), Status{RunId: "run-gone", Kinds: []string{"gone.kind"}}))
	require.NoError(t, pres.Stopped(context.Background(), Status{RunId: "run-gone", Kinds: []string{"gone.kind"}}))

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, IntrospectDeps{
		Lister: f.store, Status: f.w, Presence: pres,
		Liveness: MemLiveness{Live: map[string]bool{"run-p": true, "run-other": true}}, AbandonAfter: time.Minute,
	}))
	p, ok := reg.Lookup("watchbill_worker")
	require.True(t, ok)
	rows, err := p.(workerProvider).rows(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, rows, 3)
	byRun := map[string]workerRow{}
	for _, r := range rows {
		byRun[r.RunId] = r
	}
	assert.True(t, byRun["run-p"].Local)
	assert.True(t, byRun["run-p"].Alive)
	assert.Equal(t, "run-p", byRun["run-p"].Status.RunId, "the local row carries the live status")
	assert.False(t, byRun["run-other"].Local)
	assert.True(t, byRun["run-other"].Alive)
	assert.EqualValues(t, 4, byRun["run-other"].MaxWorkers)
	assert.False(t, byRun["run-gone"].Alive)

	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	assert.EqualValues(t, 3, rec.NumRows())
	rec.Release()

	f.w.Stop()
	require.Len(t, pres.Rows, 5)
	assert.Equal(t, watchbillpresence.PhaseStopped, pres.Rows[4].Phase)
	assert.Equal(t, "run-p", pres.Rows[4].RunId)
}
