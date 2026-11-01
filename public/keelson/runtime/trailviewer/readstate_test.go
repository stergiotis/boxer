package trailviewer

import (
	"context"
	"iter"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
)

// The observer is a pure function of the cache's fields, and every state
// it can produce is reachable in the declared graph from idle.
func TestObserveReadStateOverTheCombinations(t *testing.T) {
	type obs = readObservation
	cases := []struct {
		name string
		o    obs
		want readStateE
	}{
		{"not persisted", obs{}, readIdle},
		{"not persisted, running", obs{running: true}, readIdle},
		{"not persisted, hold", obs{hold: true}, readIdle},
		{"nothing asked", obs{persisted: true}, readIdle},
		{"first fill in flight", obs{persisted: true, running: true}, readReading},
		{"first fill failed", obs{persisted: true, failed: true}, readFailed},
		{"first fill failed, retrying", obs{persisted: true, failed: true, running: true}, readReading},
		{"rows", obs{persisted: true, filled: true, sameScope: true, rows: 3}, readRows},
		{"empty", obs{persisted: true, filled: true, sameScope: true}, readEmpty},
		{"refill failed, rows held", obs{persisted: true, filled: true, sameScope: true, failed: true, rows: 3}, readFailed},
		{"refill in flight", obs{persisted: true, filled: true, sameScope: true, running: true, rows: 3}, readRefreshing},
		{"extension in flight", obs{persisted: true, filled: true, sameScope: true, running: true, rows: 2000}, readRefreshing},
		{"refill in flight after a failure", obs{persisted: true, filled: true, sameScope: true, running: true, failed: true}, readRefreshing},
		{"scope changed, read pending", obs{persisted: true, filled: true, rows: 3}, readReading},
		{"scope changed, read in flight", obs{persisted: true, filled: true, running: true, rows: 3}, readReading},
		{"scope changed, read failed", obs{persisted: true, filled: true, failed: true, rows: 3}, readFailed},
		{"stopped with rows", obs{persisted: true, filled: true, sameScope: true, hold: true, rows: 3}, readStopped},
		{"stopped before the first fill", obs{persisted: true, hold: true}, readStopped},
		{"stopped while the read winds down", obs{persisted: true, filled: true, sameScope: true, hold: true, running: true}, readStopped},
		{"stopped after a failure", obs{persisted: true, filled: true, sameScope: true, hold: true, failed: true}, readStopped},
	}
	m := newReadFSM()
	seen := map[readStateE]bool{}
	for _, tc := range cases {
		got := observeReadState(tc.o)
		assert.Equal(t, tc.want, got, tc.name)
		seen[got] = true
	}
	for s := range seen {
		if s == readIdle {
			continue
		}
		assert.True(t, m.CanReach(readIdle, s), "%s is reachable from idle", s)
	}
	assert.Len(t, seen, len(readStateNames), "the table produces every state")
	for _, s := range []readStateE{readRows, readEmpty, readFailed} {
		assert.True(t, m.CanReach(readStopped, s), "a stop is left for %s", s)
		assert.True(t, m.CanReach(s, readStopped), "%s can stop", s)
	}
}

// blockingExec answers no query until its context ends, as a store that is
// away does, so a read stays in flight until it is stopped.
type blockingExec struct {
	fakeExec
}

func (inst *blockingExec) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	inst.mu.Lock()
	inst.sqls = append(inst.sqls, sql)
	inst.mu.Unlock()
	return func(yield func(arrow.RecordBatch, error) bool) {
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

func settled(t *testing.T, inst *App) {
	t.Helper()
	require.Eventually(t, func() bool { return !inst.cache.runner.Running() }, 5*time.Second, time.Millisecond)
}

func readStateOf(t *testing.T, inst *App) string {
	t.Helper()
	st, err := query[struct{}, opfsm.State](t, inst, opReadState, struct{}{})
	require.NoError(t, err)
	return st.Current
}

// stop_read cancels the read in flight and holds the cache: a stop is not
// a failure; nothing starts until refresh or a scope change; a second stop
// is the first.
func TestStopReadHoldsTheCacheUntilRefreshOrAScopeChange(t *testing.T) {
	exec := &fakeExec{}
	prev := sourceRef.Load()
	Register(&Source{Exec: exec, Trail: fakeTrail{}, Run: func() string { return fixtureRun }})
	t.Cleanup(func() { Register(prev) })
	inst := newInstance(manifest)
	now := time.Now()
	assert.Equal(t, "idle", readStateOf(t, inst))
	inst.tick(now)
	assert.Equal(t, "reading", readStateOf(t, inst))
	settled(t, inst)
	inst.tick(now)
	require.True(t, inst.cache.filled)
	assert.Equal(t, "empty", readStateOf(t, inst), "the fake answers no rows")
	read := len(exec.statements())

	require.NoError(t, command(t, inst, opStopRead, struct{}{}))
	inst.tick(now.Add(3 * refreshInterval()))
	assert.False(t, inst.cache.runner.Running(), "the interval refresh does not start")
	assert.Equal(t, read, len(exec.statements()))
	assert.NoError(t, inst.cache.lastErr)
	assert.Equal(t, "stopped", readStateOf(t, inst))
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "stopped", v.ReadState)
	assert.Equal(t, 0, v.Held)

	inst.cache.extendNow, inst.cache.more = true, true
	inst.tick(now.Add(4 * refreshInterval()))
	assert.False(t, inst.cache.runner.Running(), "the table's end starts no extension")
	inst.cache.extendNow, inst.cache.more = false, false

	require.NoError(t, command(t, inst, opStopRead, struct{}{}), "a second stop is the first")
	inst.tick(now.Add(5 * refreshInterval()))
	assert.True(t, inst.cache.hold)
	assert.Equal(t, "stopped", readStateOf(t, inst))

	require.NoError(t, command(t, inst, opRefresh, struct{}{}))
	inst.tick(now.Add(5 * refreshInterval()))
	assert.True(t, inst.cache.runner.Running(), "refresh lifts the hold and a read starts")
	assert.Equal(t, "refreshing", readStateOf(t, inst))
	settled(t, inst)
	inst.tick(now.Add(5 * refreshInterval()))
	assert.Equal(t, "empty", readStateOf(t, inst))
	assert.Greater(t, len(exec.statements()), read)

	require.NoError(t, command(t, inst, opStopRead, struct{}{}))
	inst.tick(now.Add(6 * refreshInterval()))
	assert.Equal(t, "stopped", readStateOf(t, inst))
	require.NoError(t, command(t, inst, opSetRange, RangeArgs{Last: "15m"}))
	scoped := now.Add(7 * refreshInterval())
	inst.tick(scoped)
	assert.False(t, inst.cache.hold, "a scope change lifts the hold")
	assert.False(t, inst.cache.runner.Running(), "the scope settles first")
	assert.Equal(t, "reading", readStateOf(t, inst), "the previous read shows while the new scope's is pending")
	assert.Equal(t, "showing the previous read", inst.freshness())
	inst.tick(scoped.Add(scopeSettle))
	assert.True(t, inst.cache.runner.Running())
	assert.Equal(t, "reading", readStateOf(t, inst))
	settled(t, inst)

	// The machine's graph, as an agent reads it.
	m, err := query[struct{}, opfsm.Machine](t, inst, opReadMachine, struct{}{})
	require.NoError(t, err)
	assert.ElementsMatch(t, readStateNames[:], m.States)
	st, err := query[struct{}, opfsm.State](t, inst, opReadState, struct{}{})
	require.NoError(t, err)
	assert.NotEmpty(t, st.History)
}

// A stop while the store is away: the worker finishes cancelled, not
// failed, so the state reads stopped and no error line appears.
func TestAStopOfAReadInFlightIsNotAFailure(t *testing.T) {
	exec := &blockingExec{}
	prev := sourceRef.Load()
	Register(&Source{Exec: exec, Trail: fakeTrail{}, Run: func() string { return fixtureRun }})
	t.Cleanup(func() { Register(prev) })
	inst := newInstance(manifest)
	now := time.Now()
	inst.tick(now)
	require.True(t, inst.cache.runner.Running())
	assert.Equal(t, "reading", readStateOf(t, inst))

	require.NoError(t, command(t, inst, opStopRead, struct{}{}))
	assert.Equal(t, "reading", readStateOf(t, inst), "the machine mirrors on the frame's tick, not on the command")
	settled(t, inst)
	inst.tick(now)
	assert.Equal(t, bgjob.StateIdle, inst.cache.runner.Snapshot().State, "a cancelled compute resets to idle")
	assert.NoError(t, inst.cache.lastErr, "a stop sets no error")
	assert.False(t, inst.cache.filled)
	assert.Equal(t, "stopped", readStateOf(t, inst))
	inst.tick(now.Add(2 * refreshInterval()))
	assert.False(t, inst.cache.runner.Running(), "the first fill is not retried under a hold")

	// Stopped before any fill, Refresh reads anew.
	require.NoError(t, command(t, inst, opRefresh, struct{}{}))
	inst.tick(now.Add(2 * refreshInterval()))
	assert.True(t, inst.cache.runner.Running())
	assert.Equal(t, "reading", readStateOf(t, inst))
	inst.cache.close()
}

// The Cancel button beside the progress bar routes through stop_read as
// the person's gesture.
func TestTheCancelButtonRoutesThroughTheCatalog(t *testing.T) {
	inst, _ := opsFixture(t)
	w := newEngineWindow(inst)
	w.frame(func() { w.inst.stopReadGesture() })
	assert.True(t, inst.cache.hold)
	log := w.eng.Log()
	assert.Equal(t, opStopRead, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)
	assert.Empty(t, w.eng.UndeclaredWrites(0))
	w.frame(func() { w.inst.refreshGesture() })
	assert.False(t, inst.cache.hold)
	assert.True(t, inst.cache.refreshNow)
}

// The read reports one step per statement it runs, in order, and finishes
// at its step count; an extension has fewer steps than a refill.
func TestTheReadReportsAStepPerStatement(t *testing.T) {
	exec := &fakeExec{}
	src := &Source{Exec: exec, Trail: fakeTrail{}, Run: func() string { return fixtureRun }}
	p, err := newProbe(src, string(manifest.Id), 1)
	require.NoError(t, err)
	from, to := t0, t0.Add(time.Hour)
	var notes []string
	var dones, totals []uint64
	report := bgjob.Reporter(func(done uint64, total uint64, note string) {
		notes, dones, totals = append(notes, note), append(dones, done), append(totals, total)
	})
	pl := readPlan{want: scope{Case: Case{Kind: CaseRun, Value: fixtureRun}}, q: listQuery{Case: Case{Kind: CaseRun, Value: fixtureRun}, From: from, To: to, Limit: 10},
		from: from, to: to, resolvedAt: to, buckets: 60, by: StripByKind, look: time.Hour}
	out, err := pl.run(context.Background(), report, p)
	require.NoError(t, err)
	require.NotNil(t, out)
	want := []string{stepRows, stepAsOf, stepStrip, stepContext}
	for _, f := range AllFacets {
		want = append(want, stepFacet+f.String())
	}
	assert.Equal(t, append(want, ""), notes, "the rules run over no rows, so their steps report nothing")
	assert.Len(t, exec.statements(), 3+len(AllFacets), "rows, strip, the context band's buckets, one per facet; the as-of read runs no SQL outside the entity case")
	assert.Equal(t, contextFactor*time.Hour, out.ctxTo.Sub(out.ctxFrom), "the plan named no extent, so the context is placed around the window")
	assert.False(t, out.ctxTo.After(to.Add(out.ctxStep)), "clamped at the read's now plus one step")
	assert.Equal(t, pl.steps(), totals[0])
	assert.Equal(t, pl.steps(), dones[len(dones)-1], "finishes at its step count")
	for i := 1; i < len(dones); i++ {
		assert.Greater(t, dones[i], dones[i-1], "the steps advance")
		assert.Equal(t, totals[0], totals[i], "the total is fixed for the read")
	}

	notes = nil
	pl.extend = true
	pl.q.Cursor = &Cursor{TsNs: from.UnixNano(), Id: 1}
	_, err = pl.run(context.Background(), report, p)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), pl.steps())
	assert.Equal(t, []string{stepRows, ""}, notes)
}
