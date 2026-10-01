package play

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stretchr/testify/require"
)

// readonlyServer plays a ClickHouse user held at a readonly level: it
// refuses, with READONLY, a request carrying a setting that level refuses,
// answers the level probe (or fails it when probeFails), and serves an
// empty Arrow stream otherwise. It records what it accepted and refused.
type readonlyServer struct {
	mu       sync.Mutex
	accepted []url.Values
	refused  int
	probes   int
}

func newReadonlyServer(t *testing.T, level string, probeFails bool) (srv *httptest.Server, rec *readonlyServer) {
	t.Helper()
	body := emptyArrowStream(t)
	refuses := map[string][]string{
		"1": {"readonly", "log_comment", "send_progress_in_http_headers"},
		"2": {"readonly"},
	}[level]
	rec = &readonlyServer{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sql, _ := io.ReadAll(r.Body)
		q := r.URL.Query()
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if strings.Contains(string(sql), "getSetting('readonly')") {
			rec.probes++
			if probeFails {
				http.Error(w, "Code: 46. DB::Exception: Unknown function getSetting. (UNKNOWN_FUNCTION)", http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, level+"\n")
			return
		}
		for _, k := range refuses {
			if q.Has(k) {
				rec.refused++
				http.Error(w, "Code: 164. DB::Exception: Cannot modify '"+k+"' setting in readonly mode. (READONLY)", http.StatusInternalServerError)
				return
			}
		}
		rec.accepted = append(rec.accepted, q)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return
}

func runOnce(c *Client) (err error) {
	const sql = `SELECT 1`
	opts := &ExecOptions{QueryID: "q1", OnProgress: func(runstream.Progress) {}}
	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), opts, nil, c.Dispatch(sql, ""))
	if err != nil {
		return
	}
	_ = closer.Close()
	rdr.Release()
	return
}

// A writable server sees the run as before and never the probe. A read-only
// one refuses the first run once; play learns the level, sends the run again
// without what that level refuses, and sends later runs degraded up front.
func TestArrowRunDegradesAfterAReadonlyRefusal(t *testing.T) {
	for _, tc := range []struct {
		level                   string
		wantProbes, wantRefused int
		wantReadonly            string
		wantStamp, wantProgress bool
		wantNote                string
	}{
		{"0", 0, 0, "2", true, true, ""},
		{"1", 1, 1, "", false, false, "read-only user, runs unstamped"},
		{"2", 1, 1, "", true, true, "read-only user"},
	} {
		srv, rec := newReadonlyServer(t, tc.level, false)
		c := NewClient(ClientConfig{URL: srv.URL}, nil)
		c.SetStampIdentity("run-ro", "app-ro", 1)
		require.NoError(t, runOnce(c), "level %s", tc.level)
		require.NoError(t, runOnce(c), "level %s", tc.level)

		require.Equal(t, tc.wantProbes, rec.probes, "level %s: probes", tc.level)
		require.Equal(t, tc.wantRefused, rec.refused, "level %s: refusals", tc.level)
		require.Len(t, rec.accepted, 2, "level %s", tc.level)
		for _, q := range rec.accepted {
			require.Equal(t, tc.wantReadonly, q.Get("readonly"), "level %s", tc.level)
			require.Equal(t, tc.wantStamp, q.Has("log_comment"), "level %s: log_comment", tc.level)
			require.Equal(t, tc.wantProgress, q.Has("send_progress_in_http_headers"), "level %s: progress", tc.level)
			require.Equal(t, "q1", q.Get("query_id"), "level %s: the run keeps its identity", tc.level)
		}
		require.Equal(t, tc.wantNote, c.ReadonlyNote(srv.URL), "level %s", tc.level)
	}
}

// When the level cannot be learned, nothing is dropped: the run fails with
// the server's own READONLY diagnostic.
func TestReadonlyRefusalWithoutALevelKeepsTheRunAsIs(t *testing.T) {
	srv, rec := newReadonlyServer(t, "1", true)
	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	err := runOnce(c)
	require.Error(t, err)
	require.Contains(t, err.Error(), "READONLY")
	require.Equal(t, 1, rec.probes)
	require.Empty(t, rec.accepted)
	require.Empty(t, c.ReadonlyNote(srv.URL))
}

// The diagnostics probe degrades the same way: its verdict is the
// statement's, not the stamp's refusal.
func TestProbeStatementDegradesAfterAReadonlyRefusal(t *testing.T) {
	srv, rec := newReadonlyServer(t, "1", false)
	c := NewClient(ClientConfig{URL: srv.URL}, nil)
	c.SetStampIdentity("run-ro", "app-ro", 1)
	const sql = `EXPLAIN AST SELECT 1`
	require.NoError(t, c.ProbeStatement(context.Background(), sql, nil, nil, c.Dispatch(sql, "")))
	require.Equal(t, 1, rec.refused)
	require.Len(t, rec.accepted, 1)
	require.False(t, rec.accepted[0].Has("log_comment"))
}

func TestIsReadonlyRefusal(t *testing.T) {
	require.True(t, isReadonlyRefusal(errString("clickhouse http 500: Code: 164. DB::Exception: Cannot modify 'log_comment' setting in readonly mode. (READONLY)")))
	require.False(t, isReadonlyRefusal(errString("clickhouse http 500: Code: 60. DB::Exception: Unknown table. (UNKNOWN_TABLE)")))
}

type errString string

func (e errString) Error() string { return string(e) }
