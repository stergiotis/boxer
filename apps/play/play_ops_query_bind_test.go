package play

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// query binds an ad-hoc alias its SQL reads and the window has not bound,
// then judges the run under the agent limits once the dataset is bound, and
// runs it.
func TestQueryBindsTheAdHocAliasItReads(t *testing.T) {
	l, h, publisher := bindLauncher(t)
	publishInts(t, publisher, "ints")
	const sql = "SELECT sum(v) FROM keelson('ints')"
	assert.Equal(t, []string{"ints"}, l.inner.unboundDatasets(sql))

	settle := func(call app.OperationCall) (settled bool, err error) {
		settled, _, err = l.Operations().(app.OperationsAsyncI).SettleAsync(call, opQuery, nil)
		return
	}
	args, err := buscodec.Encode(QueryArgs{Sql: sql})
	require.NoError(t, err)

	// Without the dataset's destination the run is refused once bound,
	// naming it.
	refused := app.OperationCall{Writer: "task:t", Key: "q1", OnBehalfOf: &app.OnBehalfOf{Task: "t"}}
	_, err = h.ApplyCommand(refused, opQuery, args)
	require.NoError(t, err, "the call is accepted while the alias binds")
	require.NotNil(t, l.follower)
	waitBound(t, l, "ints")
	settled, err := settle(refused)
	require.True(t, settled)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Destinations, "keelson:ints")
	assert.Empty(t, l.inner.unboundDatasets(sql), "bound now")

	// Covered, the run starts: the buffer holds the SQL and a run is asked.
	covered := app.OperationCall{Writer: "task:t", Key: "q2", OnBehalfOf: &app.OnBehalfOf{Task: "t", Destinations: []string{"keelson:ints", "clickhouse:ch.example:8123"}}}
	_, err = h.ApplyCommand(covered, opQuery, args)
	require.NoError(t, err)
	assert.Equal(t, sql, l.inner.sql)
	assert.True(t, l.inner.requestRun)
}

// The buffer a query takes once the alias it reads is bound is the task's
// change: the engine logs it with the task as writer, never as the person's
// edit, and the run it starts is the task's.
func TestABindingQuerysBufferIsTheTasksChange(t *testing.T) {
	l, h, publisher := bindLauncher(t)
	publishInts(t, publisher, "ints")
	eng := opengine.New(t.Context(), playOps.Catalog(), h)
	frame := func() {
		eng.BeginFrame()
		eng.ApplyQueued()
		eng.TakeSnapshot()
		if l.follower != nil {
			l.follower.Sync(l.inner)
		}
		eng.EndFrame()
	}
	frame()
	const sql = "SELECT sum(v) FROM keelson('ints')"
	obo := &app.OnBehalfOf{Task: "t", Destinations: []string{"keelson:ints", "clickhouse:ch.example:8123"}}
	args := mustEncode(t, QueryArgs{Sql: sql})
	out := eng.Submit(t.Context(), opQuery, opwire.CallRequest{CallId: "q", Writer: "task:t", Key: "q", Args: args, OnBehalfOf: obo,
		Expects: map[string]uint64{opsResSql: eng.Revisions()[opsResSql], opsResResult: eng.Revisions()[opsResResult]}})
	require.Equal(t, opwire.PhaseAccepted, out.Phase, out.Reason)
	since := eng.LogSeq()
	require.Eventually(t, func() bool {
		frame()
		st, _ := eng.Status("q")
		require.NotEqual(t, opwire.PhaseConflict, st.Phase, st.Reason)
		require.NotEqual(t, opwire.PhaseRefused, st.Phase, st.Reason)
		return l.inner.sql == sql
	}, 5*time.Second, 5*time.Millisecond, "the buffer takes the SQL once the alias is bound")
	frame()
	var sqlChanges int
	for _, e := range eng.LogSince(since) {
		if slices.Contains(e.Resources, opsResSql) {
			sqlChanges++
			assert.Equal(t, "task:t", e.Writer, "change %d to sql is the task's", e.Seq)
		}
	}
	assert.NotZero(t, sqlChanges)
	assert.Equal(t, "task:t", eng.Writer(opsResSql))
	assert.Equal(t, obo, l.inner.AgentMark(), "the run it asked for is the task's")
}

// query refuses, once its wait ends, an alias nothing is published under.
func TestQueryRefusesAnAliasThatStaysUnbound(t *testing.T) {
	l, h, _ := bindLauncher(t)
	args, err := buscodec.Encode(QueryArgs{Sql: "SELECT * FROM keelson('nothing_here')"})
	require.NoError(t, err)
	call := app.OperationCall{Writer: "task:t", Key: "q", OnBehalfOf: &app.OnBehalfOf{Task: "t"}}
	_, err = h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	q := l.inner.queryRuns["q"]
	require.Equal(t, []string{"nothing_here"}, q.binding)
	q.bindBy = time.Now().Add(-time.Second)
	l.inner.queryRuns["q"] = q
	settled, _, err := h.(app.OperationsAsyncI).SettleAsync(call, opQuery, nil)
	require.True(t, settled)
	require.ErrorContains(t, err, "no live dataset under nothing_here")
	assert.False(t, l.inner.requestRun, "nothing ran")
}

// publish_result answers once the bundle is live, with its revision.
func TestPublishResultSettlesWithTheRevision(t *testing.T) {
	fakeAppletDocs(t)
	l, h, _ := bundleLauncher(t)
	mainIntResult(t, l, runstream.Terminal{}, 0, 1, 2)
	obo := agentCall
	call := app.OperationCall{Writer: "task:t", Key: "p", OnBehalfOf: &obo}
	_, err := h.ApplyCommand(call, opPublishResult, mustEncode(t, PublishResultArgs{Bundle: "counts"}))
	require.NoError(t, err)
	var out PublishResultOutcome
	require.Eventually(t, func() bool {
		settled, raw, sErr := h.(app.OperationsAsyncI).SettleAsync(call, opPublishResult, nil)
		require.NoError(t, sErr)
		if settled {
			out, sErr = buscodec.Decode[PublishResultOutcome](raw)
			require.NoError(t, sErr)
		}
		return settled
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, "counts__result", out.Dataset)
	assert.Equal(t, uint64(1), out.Revision)
	assert.Equal(t, publishFollowing, out.Following)
}
