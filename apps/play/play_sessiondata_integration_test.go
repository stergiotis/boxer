//go:build integration

package play

import (
	"bytes"
	"context"
	"net/http"
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

// liveTable is a table of the server with rows: the largest outside the
// system databases.
func liveTable(t *testing.T, cfg chclient.Config) (table string) {
	t.Helper()
	c := NewClient(ClientConfig{URL: cfg.URL, User: cfg.User, Password: cfg.Password}, nil)
	raw, err := c.queryTabSeparated(t.Context(), "SELECT concat(database, '.', name) FROM system.tables WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') AND NOT is_temporary AND total_rows > 0 ORDER BY total_rows DESC LIMIT 1 FORMAT TabSeparated", nil)
	require.NoError(t, err)
	table = strings.TrimSpace(string(raw))
	if table == "" {
		t.Skip("the server holds no table with rows")
	}
	return
}

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
	args := mustEncode(t, QueryArgs{Sql: "SELECT * FROM " + r.table + " LIMIT 1000", KeepAs: "sample_rows_kept", Limit: 5})
	_, err := h.ApplyCommand(call, opQuery, args)
	require.NoError(t, err)
	settleUntil(t, h, call, opQuery, args, func(bool) bool { return l.inner.queryRuns[call.Key].kept != nil })
	kept := l.inner.queryRuns[call.Key].kept
	assert.Equal(t, uint64(1000), kept.Rows)
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
