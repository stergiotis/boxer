package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// renderHeadless draws one frame of body under the discard channel.
func renderHeadless(t *testing.T, body func()) {
	t.Helper()
	assert.NotPanics(t, func() {
		defer c.CurrentApplicationState.StateManager.ScriptReset()
		body()
	})
}

// settleUntil settles an asynchronous call until done says it may stop, or
// two seconds pass.
func settleUntil(t *testing.T, h app.OperationsHandlerI, call app.OperationCall, op string, args []byte, done func(settled bool) bool) (settled bool, result []byte, err error) {
	t.Helper()
	async := h.(app.OperationsAsyncI)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		settled, result, err = async.SettleAsync(call, op, args)
		if done(settled) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not get there in time", op)
	return
}

func TestQueryKeepAsKeepsADataSetAndSamplesIt(t *testing.T) {
	r := newSdRig(t)
	l, h := opsLauncher(t)
	l.inner.client = r.client("github.com/x/play", 1)
	server, session := DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: []string{server}}
	r.grant("t1", server, session)
	call := app.OperationCall{Writer: "task:t1", OnBehalfOf: obo, Key: "k1"}
	keep := func(call app.OperationCall, in QueryArgs) error {
		_, err := h.ApplyCommand(call, opQuery, mustEncode(t, in))
		return err
	}
	var refusal *app.OperationRefusal

	require.ErrorAs(t, keep(call, QueryArgs{Sql: "SELECT 1 AS x", KeepAs: "1msgs"}), &refusal)
	require.ErrorAs(t, keep(call, QueryArgs{Sql: "SELECT 1 AS x", OrderBy: []string{"x"}}), &refusal, "order_by without keep_as")
	err := keep(call, QueryArgs{Sql: "SELECT 1 AS x", KeepAs: "msgs"})
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{session}, refusal.Destinations, "the refusal names what the grant must list")
	assert.Equal(t, "SELECT 1", l.inner.sql, "a refused call leaves the buffer")

	obo.Destinations = append(obo.Destinations, session)
	args := mustEncode(t, QueryArgs{Sql: "SELECT 1 AS x", KeepAs: "msgs", Limit: 5})
	_, err = h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	ok, why := h.Snapshot().Available(opQuery)
	if !ok {
		assert.Equal(t, "a data set is being kept", why)
	}
	settleUntil(t, h, call, opQuery, args, func(bool) bool { return l.inner.queryRuns[call.Key].kept != nil })
	q := l.inner.queryRuns[call.Key]
	assert.Equal(t, "msgs", q.kept.Name)
	assert.Equal(t, "session('msgs')", q.kept.ReadWith)
	assert.Equal(t, uint64(1), q.kept.Revision)
	assert.Equal(t, uint64(3), q.kept.Rows)
	assert.Equal(t, []ColumnInfo{{Name: "x", Type: "UInt64"}}, q.kept.Columns)
	assert.Equal(t, server, q.kept.Endpoint)
	assert.Equal(t, "SELECT * FROM session('msgs') LIMIT 5", l.inner.sql, "the window samples the data set as its main run")
	assert.True(t, l.inner.requestRun)
	assert.Equal(t, obo, l.inner.AgentMark())
	assert.NotContains(t, l.inner.sessions.digest(), "sds_")

	// Listing shows the data set in its home, with its size and state.
	raw, _, err := h.Snapshot().ExternalRead(call, opListDatasets, mustEncode(t, DatasetArgs{}))
	require.NoError(t, err)
	list, err := buscodec.Decode[DatasetList](raw)
	require.NoError(t, err)
	require.Len(t, list.Datasets, 1)
	d := list.Datasets[0]
	assert.Equal(t, DatasetInfo{Name: "msgs", Home: datasetHomeSession, ReadWith: "session('msgs')", Destination: server, Granted: true,
		Columns: []ColumnInfo{{Name: "x", Type: "UInt64"}}, Rows: 3, Bytes: 24, Revision: 1, State: "live", Endpoint: server}, d)
	raw, _, err = h.Snapshot().ExternalRead(call, opListDatasets, mustEncode(t, DatasetArgs{Name: "msgs"}))
	require.NoError(t, err)
	list, err = buscodec.Decode[DatasetList](raw)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1 AS x", list.Datasets[0].Definition, "asked for by name, with its definition")
	_, _, err = h.Snapshot().ExternalRead(call, opListDatasets, mustEncode(t, DatasetArgs{Name: "nope"}))
	require.ErrorAs(t, err, &refusal)

	// A data set reading it is left stale by a redefinition, and the call
	// that redefined it says so.
	_, err = l.inner.client.defineSession(t.Context(), obo, "top", "SELECT * FROM session('msgs')", nil)
	require.NoError(t, err)
	call2 := app.OperationCall{Writer: "task:t1", OnBehalfOf: obo, Key: "k2"}
	args2 := mustEncode(t, QueryArgs{Sql: "SELECT 2 AS x", KeepAs: "msgs"})
	_, err = h.ApplyCommand(call2, opQuery, args2)
	require.NoError(t, err)
	settleUntil(t, h, call2, opQuery, args2, func(bool) bool { return l.inner.queryRuns[call2.Key].kept != nil })
	q = l.inner.queryRuns[call2.Key]
	assert.Equal(t, uint64(2), q.kept.Revision)
	assert.Equal(t, []string{"top"}, q.kept.Stale)

	// drop_dataset drops it with what reads it, when cascaded.
	dropCall := app.OperationCall{Writer: "task:t1", OnBehalfOf: obo, Key: "k3"}
	dropArgs := mustEncode(t, DropDatasetArgs{Name: "msgs"})
	_, err = h.ApplyCommand(dropCall, opDropDataset, dropArgs)
	require.NoError(t, err)
	_, _, err = settleUntil(t, h, dropCall, opDropDataset, dropArgs, func(s bool) bool { l.inner.latchSessions(); return s })
	require.ErrorAs(t, err, &refusal, "others read it")
	assert.Contains(t, err.Error(), "top")
	dropArgs = mustEncode(t, DropDatasetArgs{Name: "msgs", Cascade: true})
	_, err = h.ApplyCommand(dropCall, opDropDataset, dropArgs)
	require.NoError(t, err)
	_, raw, err = settleUntil(t, h, dropCall, opDropDataset, dropArgs, func(s bool) bool { l.inner.latchSessions(); return s })
	require.NoError(t, err)
	dropped, err := buscodec.Decode[DropDatasetResult](raw)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"msgs@2", "top@1"}, dropped.Dropped)
}

func TestKeepFailureSettlesAsARefusalNamingTheDestination(t *testing.T) {
	r := newSdRig(t)
	l, h := opsLauncher(t)
	l.inner.client = r.client("github.com/x/play", 1)
	server, session := DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)
	// The call's grant lists both, the dispatcher's does not: the service
	// refuses, and the settled call names what is missing.
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: []string{server, session}}
	r.grant("t1", server)
	call := app.OperationCall{Writer: "task:t1", OnBehalfOf: obo, Key: "k1"}
	args := mustEncode(t, QueryArgs{Sql: "SELECT 1 AS x", KeepAs: "msgs"})
	_, err := h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	settled, _, err := settleUntil(t, h, call, opQuery, args, func(s bool) bool { return s })
	require.True(t, settled)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{session}, refusal.Destinations)
}

func TestTheDataSetsTabRendersHeadless(t *testing.T) {
	t.Cleanup(egui2test.Install())
	r := newSdRig(t)
	cl := r.client("github.com/x/play", 1)
	_, err := cl.defineSession(t.Context(), nil, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	inst := NewPlayApp(cl, newLiveQueryGraph(cl, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	inst.refreshSessions()
	require.Eventually(t, func() bool { infos, _, _, _ := inst.sessions.read(); return len(infos) == 1 }, 2*time.Second, 10*time.Millisecond)
	renderHeadless(t, func() { inst.renderDatasetsTab() })
	inst.keepName = "bad name"
	inst.personKeep()
	assert.Contains(t, inst.keepNote, "identifier")
	inst.keepName = "second"
	inst.personKeep()
	require.NotNil(t, inst.keepBusy)
	require.Eventually(t, func() bool { inst.takeKeepResult(); return inst.keepBusy == nil }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, inst.keepNote, "kept second r1")
	renderHeadless(t, func() { inst.renderDatasetsTab() })

	bare := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	renderHeadless(t, func() { bare.renderDatasetsTab() })
}

// TestALaunchAsksForTheRightToKeepDataSets: a window opened for a task asks
// for its endpoint and for keeping session data sets on it, in one request.
func TestALaunchAsksForTheRightToKeepDataSets(t *testing.T) {
	host := endpointHost(chclient.ConfigFromEnv().URL)
	m := (&PlayLauncher{}).Manifest()
	require.NotNil(t, m.LaunchDestinations)
	assert.Equal(t, []string{"clickhouse:" + host, "clickhouse-session:" + host}, m.LaunchDestinations())
	assert.Empty(t, app.WhenToUseProblem(m.WhenToUse))
	assert.Contains(t, m.WhenToUse, "keep_as")
}
