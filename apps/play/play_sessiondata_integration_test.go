//go:build integration

package play

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
)

// These run play's session data path against the server the endpoint
// variables name (chclient.ConfigFromEnv), and skip when it does not
// answer. They read the server's largest table outside the system
// databases and create session temporary tables only.

// liveRig is sdRig over the live server, with the dataset service of the
// host for keelson('…').
type liveRig struct {
	sdRig
	adhoc *adhocdata.Service
	table string
	url   string
}

func newLiveRig(t *testing.T, limits sessiondata.Limits) (r *liveRig) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(strings.TrimRight(cfg.URL, "/") + "/ping")
	if err != nil {
		t.Skipf("no ClickHouse server at %s: %v", cfg.URL, err)
	}
	_ = resp.Body.Close()
	adhoc, err := adhocdata.NewService(adhocdata.Config{Registry: introspect.NewRegistry(), Dir: t.TempDir(), Log: zerolog.Nop()})
	require.NoError(t, err)
	r = &liveRig{adhoc: adhoc, url: cfg.URL}
	r.t, r.bus, r.disp = t, inprocbus.NewInst(zerolog.Nop()), &sdDispatcher{dests: map[string][]string{}}
	svc, err := sessiondata.NewService(sessiondata.Config{Bus: r.bus, Log: zerolog.Nop(), SweepInterval: -1, Adhoc: adhoc, Limits: limits})
	require.NoError(t, err)
	svc.SetDispatcher(r.disp)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	r.svc, r.host = svc, endpointHost(cfg.URL)
	r.table = liveTable(t, cfg)
	return
}

// liveTable is a table of the server a statement reads a thousand rows
// of: the largest outside the system databases that holds them. The rows
// are counted, not taken from system.tables, whose total_rows still counts
// a row a lightweight delete has masked.
func liveTable(t *testing.T, cfg chclient.Config) (table string) {
	t.Helper()
	c := NewClient(ClientConfig{URL: cfg.URL, User: cfg.User, Password: cfg.Password}, nil)
	raw, err := c.queryTabSeparated(t.Context(), "SELECT concat(database, '.', name) FROM system.tables WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') AND NOT is_temporary AND total_rows >= "+strconv.Itoa(liveTableRows)+" ORDER BY total_rows DESC LIMIT 8 FORMAT TabSeparated", nil)
	require.NoError(t, err)
	for _, candidate := range strings.Fields(string(raw)) {
		n, cErr := c.queryTabSeparated(t.Context(), "SELECT count() FROM (SELECT 1 FROM "+candidate+" LIMIT "+strconv.Itoa(liveTableRows)+") FORMAT TabSeparated", nil)
		if cErr == nil && strings.TrimSpace(string(n)) == strconv.Itoa(liveTableRows) {
			return candidate
		}
	}
	t.Skipf("the server holds no table with %d rows to read", liveTableRows)
	return
}

// liveTableRows is how many rows the tests read of the server's table.
const liveTableRows = 1000

func (inst *liveRig) liveClient(appId app.AppIdT, instance uint64) (c *Client) {
	cfg := chclient.ConfigFromEnv()
	c = NewClient(ClientConfig{URL: cfg.URL, User: cfg.User, Password: cfg.Password}, nil)
	c.SetSessionBus(inst.window(appId, instance))
	return
}

func TestLiveKeepThroughTheOperationAndReadItAgain(t *testing.T) {
	r := newLiveRig(t, sessiondata.Limits{})
	server, session := DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)
	r.grant("t1", server, session)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: []string{server, session}}
	l, h := opsLauncher(t)
	l.inner.client = r.liveClient("github.com/x/play", 1)

	call := app.OperationCall{Writer: "task:t1", OnBehalfOf: obo, Key: "k1"}
	args := mustEncode(t, QueryArgs{Sql: "SELECT * FROM " + r.table + " LIMIT " + strconv.Itoa(liveTableRows), KeepAs: "sample_rows_kept", Limit: 5})
	_, err := h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	settleUntil(t, h, call, opQuery, args, func(bool) bool { return l.inner.queryRuns[call.Key].kept != nil })
	kept := l.inner.queryRuns[call.Key].kept
	assert.Equal(t, uint64(liveTableRows), kept.Rows)
	assert.NotEmpty(t, kept.Columns)
	assert.Equal(t, "SELECT * FROM session('sample_rows_kept') LIMIT 5", l.inner.sql)

	// A second statement reads it, and another app of the task does too.
	other := r.liveClient("github.com/x/kernel", 9)
	obo2 := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c2", Destinations: []string{server}}
	rows, _, err := runAll(t, t.Context(), other, "SELECT count() AS n FROM session('sample_rows_kept') WHERE 1", &ExecOptions{Agent: obo2})
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows)
	rows, _, err = runAll(t, t.Context(), l.inner.client, "SELECT * FROM session('sample_rows_kept') LIMIT 7", &ExecOptions{Agent: obo2})
	require.NoError(t, err)
	assert.Equal(t, int64(7), rows)
}

func TestLiveAnAdhocDatasetJoinsAServerTable(t *testing.T) {
	r := newLiveRig(t, sessiondata.Limits{})
	_, err := r.adhoc.Publish(adhocdata.PublishInput{Alias: "items", ArrowIPCStream: liveItems(t)})
	require.NoError(t, err)
	c := r.liveClient("github.com/x/play", 1)
	c.SetResolver(autoResolver)
	// The person's own run; the endpoint is declared as one that may see
	// sealed data, as the reach proof would.
	c.reach.record(r.url)
	sql := "SELECT i.id, m.n FROM keelson('items') AS i CROSS JOIN (SELECT count() AS n FROM " + r.table + ") AS m"
	dec := c.Dispatch(sql, "")
	require.Equal(t, dispatchClassSession, dec.class, dec.reason)
	rows, confined, err := runAll(t, t.Context(), c, sql, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.True(t, confined)

	// Kept, then joined again from the data set.
	_, err = c.defineSession(t.Context(), nil, "", "items_kept", "SELECT * FROM keelson('items')", []string{"id"})
	require.NoError(t, err)
	rows, confined, err = runAll(t, t.Context(), c, "SELECT k.id FROM session('items_kept') AS k CROSS JOIN (SELECT 1 FROM "+r.table+" LIMIT 1) AS m", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.True(t, confined)
}

func TestLiveAnExpiredSessionIsReplayedThroughPlaysRunPath(t *testing.T) {
	r := newLiveRig(t, sessiondata.Limits{SessionTimeout: 2 * time.Second})
	c := r.liveClient("github.com/x/play", 1)
	_, err := c.defineSession(t.Context(), nil, "", "base", "SELECT * FROM "+r.table+" LIMIT 100", nil)
	require.NoError(t, err)
	_, err = c.defineSession(t.Context(), nil, "", "derived", "SELECT count() AS n FROM session('base')", nil)
	require.NoError(t, err)
	time.Sleep(5500 * time.Millisecond)
	rows, _, err := runAll(t, t.Context(), c, "SELECT n FROM session('derived')", nil)
	require.NoError(t, err, "the run replays what it reads, oldest first, in a new session")
	assert.Equal(t, int64(1), rows)
	infos, err := c.listSession(t.Context(), nil)
	require.NoError(t, err)
	for _, i := range infos {
		assert.Equal(t, uint64(1), i.Replays, i.Name)
	}
}

func liveItems(t *testing.T) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Uint64}, {Name: "label", Type: arrow.BinaryTypes.String}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Uint64Builder).AppendValues([]uint64{1, 2}, nil)
	rb.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// A task joins a table its own call published at TLP:CLEAR with a server
// table: the server receives it as external data, as it receives any
// ad-hoc dataset, and the result is not labelled. A dataset a person
// published beside it, the person's own run and a data set kept from the
// person's dataset keep the label.
func TestLiveATaskJoinsItsOwnClearPublicationUnlabelled(t *testing.T) {
	r := newLiveRig(t, sessiondata.Limits{})
	r.adhoc.SetDispatcher(r.disp)
	window := adhocdata.Identity{App: "github.com/x/play", Instance: 1}
	server, session := DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)
	r.grant("t1", server, session, DestinationKeelson("items"))
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: []string{server, session, DestinationKeelson("items")}}
	_, err := r.adhoc.PublishBundle(t.Context(), adhocdata.BundlePublishInput{Alias: "mine", Document: []byte("SELECT 1"), By: window, OnBehalfOf: obo,
		Datasets:   []adhocdata.BundleDatasetInput{{LocalName: "result", ArrowIPCStream: liveItems(t)}},
		Provenance: adhocdata.BundleProvenance{FromNothingSealed: true}})
	require.NoError(t, err)
	_, err = r.adhoc.Publish(adhocdata.PublishInput{Alias: "items", ArrowIPCStream: liveItems(t)})
	require.NoError(t, err)
	c := r.liveClient(window.App, window.Instance)
	c.SetResolver(autoResolver)
	c.reach.record(r.url)
	task := &ExecOptions{Agent: obo}
	counted := "CROSS JOIN (SELECT count() AS n FROM " + r.table + ") AS m"

	own := "SELECT i.id, m.n FROM keelson('mine__result') AS i " + counted
	rows, confined, err := runAll(t, t.Context(), c, own, task)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.False(t, confined, "the task's own clear publication, attached to the server and read back")

	rows, confined, err = runAll(t, t.Context(), c, own, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.True(t, confined, "the person's run of the same statement")

	rows, confined, err = runAll(t, t.Context(), c, "SELECT i.id FROM keelson('mine__result') AS i JOIN keelson('items') AS p USING id "+counted, task)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.True(t, confined, "a dataset the task did not publish beside its own")

	kept, err := c.defineSession(t.Context(), obo, "", "mine_kept", "SELECT * FROM keelson('mine__result')", []string{"id"})
	require.NoError(t, err)
	assert.True(t, kept.Dataset.Confined, "held as derived from sealed data")
	assert.True(t, kept.Dataset.OwnClear)
	rows, confined, err = runAll(t, t.Context(), c, "SELECT k.id FROM session('mine_kept') AS k "+counted, task)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.False(t, confined)

	theirs, err := c.defineSession(t.Context(), obo, "", "items_kept", "SELECT * FROM keelson('items')", []string{"id"})
	require.NoError(t, err)
	assert.False(t, theirs.Dataset.OwnClear)
	rows, confined, err = runAll(t, t.Context(), c, "SELECT k.id FROM session('mine_kept') AS k JOIN session('items_kept') AS p USING id "+counted, task)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rows)
	assert.True(t, confined, "a confined data set beside the task's own")

	// An endpoint that has not shown it may see sealed data gets neither.
	unproven := r.liveClient(window.App, window.Instance)
	unproven.SetResolver(autoResolver)
	_, _, err = runAll(t, t.Context(), unproven, own, task)
	require.Error(t, err)
	assert.ErrorContains(t, err, "sealed data")
}
