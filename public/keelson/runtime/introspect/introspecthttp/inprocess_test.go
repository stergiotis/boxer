package introspecthttp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/trivialsql"
)

type passThrough struct{ hosts []string }

func (inst *passThrough) RoundTrip(req *http.Request) (*http.Response, error) {
	inst.hosts = append(inst.hosts, req.URL.Host)
	return &http.Response{StatusCode: http.StatusTeapot, Body: http.NoBody, Request: req}, nil
}

func trivialClient(t *testing.T) (*http.Client, *passThrough) {
	t.Helper()
	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))
	srv := New(Config{Registry: reg, Runner: MacroRunnerFunc(func(ctx context.Context, sql string, params map[string]string) ([]byte, error) {
		return trivialsql.Run(ctx, reg, sql, params)
	})}, zerolog.Nop())
	next := &passThrough{}
	rt, err := InProcess("http://keelson.invalid", srv.Handler(), next)
	require.NoError(t, err)
	return &http.Client{Transport: rt}, next
}

func post(t *testing.T, c *http.Client, url, sql string) (int, string) {
	t.Helper()
	resp, err := c.Post(url, "text/plain", strings.NewReader(sql))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// A client speaking the ClickHouse HTTP dialect to the in-process origin is
// answered by the trivial evaluator, with no socket and no ClickHouse
// (ADR-0290 §SD4).
func TestInProcess_TrivialQuery(t *testing.T) {
	c, _ := trivialClient(t)
	status, body := post(t, c, "http://keelson.invalid/query?param_n=2", "SELECT * FROM keelson('env') LIMIT {n:UInt64} FORMAT TabSeparated")
	// LIMIT takes a number here, not a placeholder: refused as the shape says.
	assert.NotEqual(t, http.StatusOK, status)
	assert.Contains(t, body, "needs ClickHouse")

	status, body = post(t, c, "http://keelson.invalid/query", "SELECT * FROM keelson('env') LIMIT 2 FORMAT TabSeparated")
	require.Equal(t, http.StatusOK, status, body)
	assert.Len(t, strings.Split(strings.TrimRight(body, "\n"), "\n"), 2)
}

func TestInProcess_RefusalIsAServerError(t *testing.T) {
	c, _ := trivialClient(t)
	status, body := post(t, c, "http://keelson.invalid/query", "SELECT name FROM system.tables")
	// The chhttp envelope: plain text, and no engine code, since none exists
	// to report.
	assert.GreaterOrEqual(t, status, 400)
	assert.Contains(t, body, "needs ClickHouse")
}

func TestInProcess_OtherOriginsPassThrough(t *testing.T) {
	c, next := trivialClient(t)
	resp, err := c.Get("http://example.invalid/x")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode)
	assert.Equal(t, []string{"example.invalid"}, next.hosts)
}

func TestInProcess_RejectsAnOriginWithAPath(t *testing.T) {
	_, err := InProcess("http://keelson.invalid/query", http.NotFoundHandler(), http.DefaultTransport)
	assert.Error(t, err)
}
