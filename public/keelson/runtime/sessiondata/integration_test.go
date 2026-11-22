//go:build integration

package sessiondata

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine/chserver"
)

// These run against the server the endpoint variables name
// (chclient.ConfigFromEnv) and skip when it does not answer. They create
// session temporary tables only.

func liveEndpoint(t *testing.T) (e Endpoint) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.URL, "/")+"/ping", nil)
	require.NoError(t, err)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Skipf("no ClickHouse server at %s: %v", cfg.URL, err)
	}
	_ = resp.Body.Close()
	return Endpoint{URL: cfg.URL, User: cfg.User, Password: cfg.Password, ServesConfined: true}
}

// run sends a binding's statement the way a caller does: through the
// server engine, with readonly=2 and the binding applied.
func run(t *testing.T, e Endpoint, b Binding) (out string) {
	t.Helper()
	eng, err := chserver.New(chserver.Config{Endpoint: e.URL, User: e.User, Password: e.Password, ServesConfined: e.ServesConfined})
	require.NoError(t, err)
	req := queryengine.Request{SQL: b.SQL, Format: "TabSeparated", Settings: map[string]string{"readonly": "2"}}
	require.NoError(t, b.Apply(&req))
	st, _, err := eng.Deliver(context.Background(), req)
	require.NoError(t, err)
	var sb strings.Builder
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		sb.Write(f.Data)
		if f.Terminal.Err != nil {
			require.NoError(t, f.Terminal.Err)
		}
	}
	require.NoError(t, st.Err())
	_ = st.Close()
	return strings.TrimSpace(sb.String())
}

func TestLiveDefinePrepareReplayAndAttach(t *testing.T) {
	e := liveEndpoint(t)
	svc, err := NewService(Config{Log: zerolog.Nop(), SweepInterval: -1,
		Adhoc:  &fakeAdhoc{streams: map[string][]byte{"items": itemsStream(t)}},
		Limits: Limits{SessionTimeout: 2 * time.Second, MaxRows: 1_000_000}})
	require.NoError(t, err)
	ctx := context.Background()
	defer func() { _ = svc.Close(ctx) }()
	ticket, err := svc.Open(ctx, person)
	require.NoError(t, err)
	me := Caller{By: person}

	a, err := svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: "a", OrderBy: []string{"x"},
		SQL: "SELECT number AS x, toString(number) AS s FROM numbers(1000)"})
	require.NoError(t, err)
	assert.Equal(t, uint64(1000), a.Dataset.Rows)
	assert.Equal(t, []Column{{Name: "x", Type: "UInt64"}, {Name: "s", Type: "String"}}, a.Dataset.Columns)
	_, err = svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: "b",
		SQL: "SELECT x % 10 AS k, count() AS n FROM session('a') GROUP BY k"})
	require.NoError(t, err)

	b, err := svc.Prepare(ctx, PrepareInput{Caller: me, Ticket: ticket, Endpoint: e, SQL: "SELECT sum(n) FROM session('b')"})
	require.NoError(t, err)
	assert.Equal(t, "1000", run(t, e, b))
	require.NoError(t, svc.Release(person, b.Lease))

	// The server drops the session after its timeout; the next statement
	// replays both definitions, in order.
	time.Sleep(3500 * time.Millisecond)
	b, err = svc.Prepare(ctx, PrepareInput{Caller: me, Ticket: ticket, Endpoint: e, SQL: "SELECT sum(n) FROM session('b')"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a@1", "b@1"}, b.Replayed)
	assert.Equal(t, "1000", run(t, e, b))
	require.NoError(t, svc.Release(person, b.Lease))

	// A host dataset joined with a data set on the server.
	b, err = svc.Prepare(ctx, PrepareInput{Caller: me, Ticket: ticket, Endpoint: e,
		SQL: "SELECT i.label, a.s FROM session('a') AS a JOIN keelson('items') AS i ON a.x = i.id ORDER BY i.id"})
	require.NoError(t, err)
	assert.Equal(t, "a\t1\nb\t2", run(t, e, b))
	require.NoError(t, svc.Release(person, b.Lease))

	// A data set defined from a host dataset.
	c, err := svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: "c", SQL: "SELECT id * 10 AS y FROM keelson('items')"})
	require.NoError(t, err)
	assert.True(t, c.Dataset.Confined)
	assert.Equal(t, uint64(2), c.Dataset.Rows)
}

// TestLiveStatementsInOneSessionDoNotOverlap runs prepared statements
// concurrently; the leases keep the server from refusing one as locked.
func TestLiveStatementsInOneSessionDoNotOverlap(t *testing.T) {
	e := liveEndpoint(t)
	svc, err := NewService(Config{Log: zerolog.Nop(), SweepInterval: -1})
	require.NoError(t, err)
	ctx := context.Background()
	defer func() { _ = svc.Close(ctx) }()
	ticket, err := svc.Open(ctx, person)
	require.NoError(t, err)
	me := Caller{By: person}
	_, err = svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: "a", SQL: "SELECT number AS x FROM numbers(10)"})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, pErr := svc.Prepare(ctx, PrepareInput{Caller: me, Ticket: ticket, Endpoint: e, SQL: "SELECT sleep(0.2) + count() FROM session('a')"})
			if !assert.NoError(t, pErr) {
				return
			}
			assert.Equal(t, "10", run(t, e, b))
			assert.NoError(t, svc.Release(person, b.Lease))
		}()
	}
	wg.Wait()
}

// TestLiveANestedCommentHidesNothingFromTheCheck: the server nests block
// comments and boxer's grammar does not, so the definition as written
// would have the server evaluate an expression the check never saw. It is
// refused, and the session holds no table of it.
func TestLiveANestedCommentHidesNothingFromTheCheck(t *testing.T) {
	e := liveEndpoint(t)
	svc, err := NewService(Config{Log: zerolog.Nop(), SweepInterval: -1})
	require.NoError(t, err)
	ctx := context.Background()
	defer func() { _ = svc.Close(ctx) }()
	ticket, err := svc.Open(ctx, person)
	require.NoError(t, err)
	me := Caller{By: person}
	_, err = svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: "n", SQL: nestedComment})
	assert.ErrorContains(t, err, "not a read")
	res, err := svc.Define(ctx, DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: "m",
		SQL: "SELECT 1 /* a *//* b */ AS x, '/* */' AS s -- c\nFROM numbers(2)"})
	require.NoError(t, err, "comments both lexers read alike are dropped, not refused")
	assert.Equal(t, []Column{{Name: "x", Type: "UInt8"}, {Name: "s", Type: "String"}}, res.Dataset.Columns)
	assert.Equal(t, uint64(2), res.Dataset.Rows)
}
