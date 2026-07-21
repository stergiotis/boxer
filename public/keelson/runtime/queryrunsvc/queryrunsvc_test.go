package queryrunsvc

import (
	"context"
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

func TestIsLoopbackHost(t *testing.T) {
	require.True(t, isLoopbackHost("127.0.0.1"))
	require.True(t, isLoopbackHost("::1"))
	require.True(t, isLoopbackHost("localhost"))
	require.True(t, isLoopbackHost(""))
	require.False(t, isLoopbackHost("0.0.0.0"))
	require.False(t, isLoopbackHost("192.168.1.10"))
	require.False(t, isLoopbackHost("example.com"))
}
