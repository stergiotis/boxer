package queryrunsvc

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
)

func TestNewDefaultsAndScopeValidation(t *testing.T) {
	s, err := New(Config{}, zerolog.Nop())
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8127", s.cfg.Listen)
	require.Equal(t, "boxer.facts", s.FactsTable())
	require.Equal(t, "boxer.mv_queryruns", s.MvName())
	require.Equal(t, queryrunfacts.ScopeAll, s.cfg.Scope)
	require.Equal(t, 5, s.cadenceSeconds())

	_, err = New(Config{Scope: "everything"}, zerolog.Nop())
	require.Error(t, err)
}

func TestCadenceRoundsUpToWholeSeconds(t *testing.T) {
	s, err := New(Config{Cadence: 300 * time.Millisecond}, zerolog.Nop())
	require.NoError(t, err)
	require.Equal(t, 1, s.cadenceSeconds())
	s, err = New(Config{Cadence: 2500 * time.Millisecond}, zerolog.Nop())
	require.NoError(t, err)
	require.Equal(t, 3, s.cadenceSeconds())
}

func TestStartRefusesNonLoopback(t *testing.T) {
	s, err := New(Config{Listen: "0.0.0.0:0"}, zerolog.Nop())
	require.NoError(t, err)
	err = s.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing non-loopback")
}

// Scope off must serve a valid schema-only ArrowStream without touching
// ClickHouse — the pipeline keeps ticking while capturing nothing.
func TestPullScopeOffServesSchemaOnlyStream(t *testing.T) {
	s, err := New(Config{Scope: queryrunfacts.ScopeOff, ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.NoError(t, err)
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pull")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/vnd.apache.arrow.stream", resp.Header.Get("Content-Type"))

	rd, err := ipc.NewReader(resp.Body)
	require.NoError(t, err)
	defer rd.Release()
	require.Positive(t, rd.Schema().NumFields())
	require.False(t, rd.Next(), "no batches expected in a scope-off stream")
	require.NoError(t, rd.Err())
}

// The ETag lets a ranged continuation detect that the answer changed
// underneath it: a matching If-Range gets the range, a stale one the full
// body, never a splice.
func TestPullETagGuardsRangedReads(t *testing.T) {
	s, err := New(Config{Scope: queryrunfacts.ScopeOff, ChURL: "http://127.0.0.1:1/"}, zerolog.Nop())
	require.NoError(t, err)
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	get := func(hdr map[string]string) (resp *http.Response) {
		req, rErr := http.NewRequest(http.MethodGet, srv.URL+"/pull", nil)
		require.NoError(t, rErr)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, rErr = http.DefaultClient.Do(req)
		require.NoError(t, rErr)
		_ = resp.Body.Close()
		return
	}
	first := get(nil)
	etag := first.Header.Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, etag, get(nil).Header.Get("ETag"), "an unchanged answer keeps its ETag")

	require.Equal(t, http.StatusPartialContent, get(map[string]string{"Range": "bytes=8-", "If-Range": etag}).StatusCode)
	require.Equal(t, http.StatusOK, get(map[string]string{"Range": "bytes=8-", "If-Range": `"stale"`}).StatusCode)
}

func TestStartRefusesWildcardBind(t *testing.T) {
	s, err := New(Config{Listen: ":0"}, zerolog.Nop())
	require.NoError(t, err)
	err = s.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing non-loopback")
}

// The materialized view pulls a loopback URL from ClickHouse's side, so a
// server elsewhere can never reach it; Start must say so instead of
// succeeding into refreshes that all fail.
func TestStartRefusesRemoteClickHouse(t *testing.T) {
	s, err := New(Config{Listen: "127.0.0.1:0", ChURL: "http://10.1.2.3:8123/"}, zerolog.Nop())
	require.NoError(t, err)
	err = s.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "off this host")
}

// A serve loop that ends other than by Stop surfaces on Failed, so the
// daemon can exit and its supervisor restart it; a graceful Stop does not.
func TestServeFailureSurfaces(t *testing.T) {
	s, err := New(Config{}, zerolog.Nop())
	require.NoError(t, err)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go s.serve(ln)
	require.NoError(t, ln.Close())
	select {
	case err = <-s.Failed():
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "a dead listener must surface on Failed")
	}

	s, err = New(Config{}, zerolog.Nop())
	require.NoError(t, err)
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go s.serve(ln)
	require.NoError(t, s.Stop(context.Background()))
	select {
	case err = <-s.Failed():
		require.Failf(t, "graceful stop reported", "%v", err)
	case <-time.After(200 * time.Millisecond):
	}
}

// Everything the service sends carries the reconcile tag, so its boot DDL
// and checks stay out of what it captures.
func TestClientTagsEveryQuery(t *testing.T) {
	got := make(chan string, 1)
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query().Get("log_comment")
	}))
	defer ch.Close()
	s, err := New(Config{ChURL: ch.URL + "/"}, zerolog.Nop())
	require.NoError(t, err)
	require.NoError(t, s.cli.Exec(context.Background(), "SYSTEM FLUSH LOGS"))
	require.Equal(t, queryrunfacts.ReconcileTag, <-got)
}

func TestHealthz(t *testing.T) {
	s, err := New(Config{}, zerolog.Nop())
	require.NoError(t, err)
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// ParseBackfill resolves the operator-facing spelling. "all" must stay the
// zero time: that is what keeps the original unbounded first-boot reach for
// every existing deployment.
func TestParseBackfill(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	for _, spec := range []string{"", BackfillAll} {
		from, err := ParseBackfill(spec, now)
		require.NoError(t, err)
		require.True(t, from.IsZero(), "%q must leave the reach unbounded", spec)
	}

	from, err := ParseBackfill(BackfillNone, now)
	require.NoError(t, err)
	require.Equal(t, now, from, "none starts at service start")

	from, err = ParseBackfill("24h", now)
	require.NoError(t, err)
	require.Equal(t, now.Add(-24*time.Hour), from)

	_, err = ParseBackfill("yesterday", now)
	require.Error(t, err, "an unparseable spelling must fail loudly, not silently backfill everything")
	_, err = ParseBackfill("-1h", now)
	require.Error(t, err, "a negative duration would start in the future")
}
