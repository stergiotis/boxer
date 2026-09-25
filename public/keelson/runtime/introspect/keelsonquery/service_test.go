package keelsonquery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

const readerId app.AppIdT = "test.keelsonquery.reader"

// serve is the host's arrangement: the chlocal broker and the service on
// one bus, over the static providers.
func serve(t *testing.T) (bus *inprocbus.Inst, sink *audit.InMemoryAuditSink) {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus = inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)
	sink = audit.NewInMemoryAuditSink()
	bus.SetAuditSink(sink)
	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})
	svc, err := NewService(bus, logger, testRegistry(t), "introspect")
	require.NoError(t, err)
	svc.Timeout = 15 * time.Second
	t.Cleanup(svc.Close)
	return
}

type envCols struct {
	Name []string `ch:"name"`
}

// A reader holding the grant for one table reads it in both spellings,
// and the bus audits the request with the reader as sender.
func TestReadOverGrantedTable(t *testing.T) {
	bus, sink := serve(t)
	cli := NewClient(bus.NewClient(readerId, ClientCaps("env")))
	cli.Timeout = 15 * time.Second
	ctx := context.Background()

	var cols envCols
	n, err := Columns(ctx, cli, "env", "SELECT name FROM keelson('env') WHERE name != '' ORDER BY name LIMIT 3", &cols)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	assert.NotEmpty(t, cols.Name[0])

	res, err := cli.Query(ctx, "env", "SELECT count() AS n FROM env", "TabSeparated")
	require.NoError(t, err)
	assert.NotEmpty(t, res.Body)

	var seen bool
	for _, rec := range sink.Records() {
		if rec.Subject == Subject("env") && rec.AppId == readerId {
			seen = true
		}
	}
	assert.True(t, seen, "the read is a request the bus audits with the reader as sender")
}

// A statement reaching past the grant comes back as a refusal with the
// reason, not as rows and not as a timeout.
func TestRefusalsCarryTheirReason(t *testing.T) {
	bus, _ := serve(t)
	cli := NewClient(bus.NewClient(readerId, ClientCaps("env", "no_such_table")))
	cli.Timeout = 15 * time.Second
	ctx := context.Background()

	cases := map[string]string{
		"SELECT name FROM keelson('build')":                        "grants only env",
		"SELECT * FROM url('http://127.0.0.1:1/x', 'JSONEachRow')": "table function",
		"INSERT INTO env SELECT * FROM env":                        "read-only",
	}
	for sql, want := range cases {
		_, err := cli.Query(ctx, "env", sql, "")
		var refused *RefusedError
		require.True(t, errors.As(err, &refused), "%s: %v", sql, err)
		assert.Contains(t, refused.Reason, want, sql)
	}
	_, err := cli.Query(ctx, "no_such_table", "SELECT 1", "")
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "no introspection table")
}

// Without the grant the bus refuses the publish; the service never sees it.
func TestNoGrantIsAPermissionError(t *testing.T) {
	bus, _ := serve(t)
	cli := NewClient(bus.NewClient(readerId, ClientCaps("env")))
	cli.Timeout = 2 * time.Second
	_, err := cli.Query(context.Background(), "build", "SELECT 1 FROM build", "")
	require.Error(t, err)
	var refused *RefusedError
	assert.False(t, errors.As(err, &refused), "a permission failure is the transport's, not a reply")
}

// A placeholder bound through the request reaches the engine, and a
// request whose parameter columns disagree is refused, not truncated.
func TestParamsBindPlaceholders(t *testing.T) {
	bus, _ := serve(t)
	cli := NewClient(bus.NewClient(readerId, ClientCaps("env")))
	cli.Timeout = 15 * time.Second
	ctx := context.Background()

	var all envCols
	_, err := Columns(ctx, cli, "env", "SELECT name FROM keelson('env') WHERE name != '' ORDER BY name LIMIT 2", &all)
	require.NoError(t, err)
	require.Len(t, all.Name, 2)
	var got envCols
	_, err = ColumnsWith(ctx, cli, Request{Table: "env",
		Sql:    "SELECT name FROM keelson('env') WHERE name = {n:String}",
		Params: map[string]string{"n": all.Name[1]}}, &got)
	require.NoError(t, err)
	assert.Equal(t, []string{all.Name[1]}, got.Name)

	svc := bus.NewClient("test.keelsonquery.raw", ClientCaps("env"))
	payload, err := buscodec.Encode(keelsonqueryrequest.KeelsonQueryRequest{Table: "env", Sql: "SELECT 1 FROM env",
		ParamName: []string{"a", "b"}, ParamValue: []string{"1"}})
	require.NoError(t, err)
	raw, err := svc.RequestWithTimeout(Subject("env"), payload, 5*time.Second)
	require.NoError(t, err)
	r, err := buscodec.Decode[keelsonqueryreply.KeelsonQueryReply](raw)
	require.NoError(t, err)
	assert.False(t, r.Ok)
	assert.Contains(t, r.Reason, "differ in length")
}
