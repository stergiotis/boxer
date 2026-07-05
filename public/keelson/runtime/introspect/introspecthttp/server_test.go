package introspecthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
)

func chBin(t *testing.T) (bin string) {
	t.Helper()
	if p, err := exec.LookPath("clickhouse-local"); err == nil {
		return p
	}
	const def = "/usr/bin/clickhouse-local"
	if _, err := os.Stat(def); err == nil {
		return def
	}
	t.Skip("clickhouse-local not installed")
	return
}

func runCH(t *testing.T, bin, query string) (out string) {
	t.Helper()
	cmd := exec.Command(bin, "--query", query)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "clickhouse-local failed; stderr: %s", stderr.String())
	return strings.TrimSpace(stdout.String())
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	require.NoError(t, introspect.RegisterCatalog(r))
	s := New(Config{Registry: r}, zerolog.Nop())
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

// TestServer_UrlTableFunction is the SD3 end-to-end check: clickhouse-local
// pulls keelson.env from our HTTP endpoint via url()+ArrowStream.
func TestServer_UrlTableFunction(t *testing.T) {
	bin := chBin(t)
	s := newTestServer(t)
	out := runCH(t, bin, fmt.Sprintf(
		"SELECT count() FROM url('%s/table/env', 'ArrowStream')", s.BaseURL()))
	n, err := strconv.Atoi(out)
	require.NoError(t, err, "output: %q", out)
	assert.Positive(t, n, "env table should expose rows over url()")
}

// TestServer_MultipleUrlTables proves a single query can pull two
// keelson tables — the "clickhouse-server joins keelson tables with
// other data" path (ADR-0094 §SD3).
func TestServer_MultipleUrlTables(t *testing.T) {
	bin := chBin(t)
	_, _ = runinfo.Init() // give keelson.build a row
	s := newTestServer(t)
	q := fmt.Sprintf(
		"SELECT (SELECT count() FROM url('%[1]s/table/env','ArrowStream')) AS e, "+
			"(SELECT count() FROM url('%[1]s/table/build','ArrowStream')) AS b", s.BaseURL())
	out := runCH(t, bin, q)
	fields := strings.Fields(out)
	require.Len(t, fields, 2, "output: %q", out)
	e, _ := strconv.Atoi(fields[0])
	b, _ := strconv.Atoi(fields[1])
	assert.Positive(t, e)
	assert.Equal(t, 1, b, "build is a single-row table once runinfo is initialised")
}

// TestServer_ColsProjection verifies the ?cols= lever prunes columns —
// checked directly against the ArrowStream body (no ClickHouse needed).
func TestServer_ColsProjection(t *testing.T) {
	s := newTestServer(t)
	resp, err := http.Get(s.BaseURL() + "/table/env?cols=name")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	rdr, err := ipc.NewReader(resp.Body, ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	defer rdr.Release()
	sc := rdr.Schema()
	require.Equal(t, 1, sc.NumFields())
	assert.Equal(t, "name", sc.Field(0).Name)
}

// TestServer_CatalogViaUrl checks the system.tables-equivalent: querying
// keelson.tables over url() lists every registered table, itself included.
func TestServer_CatalogViaUrl(t *testing.T) {
	bin := chBin(t)
	s := newTestServer(t)
	out := runCH(t, bin, fmt.Sprintf(
		"SELECT name FROM url('%s/table/tables','ArrowStream') ORDER BY name", s.BaseURL()))
	for _, want := range []string{"apps", "build", "columns", "env", "sbom", "tables"} {
		assert.Contains(t, out, want, "catalog should list %q", want)
	}
}

func TestServer_UnknownTable404(t *testing.T) {
	s := newTestServer(t)
	resp, err := http.Get(s.BaseURL() + "/table/does_not_exist")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_RefusesNonLoopback(t *testing.T) {
	s := New(Config{Registry: introspect.NewRegistry(), Addr: "0.0.0.0:0"}, zerolog.Nop())
	err := s.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-loopback")
}

func TestServer_TablesListing(t *testing.T) {
	s := newTestServer(t)
	resp, err := http.Get(s.BaseURL() + "/tables")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(resp.Body)
	assert.Contains(t, body.String(), "env")
	assert.Contains(t, body.String(), "apps")
}

// newQueryServer wires a /query-capable server backed by a real chlocal
// broker (skips when clickhouse-local is absent).
func newQueryServer(t *testing.T) *Server {
	t.Helper()
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	caller := bus.NewClient("test.introspect.query", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	runner := RunnerFunc(func(ctx context.Context, sql string) ([]byte, error) {
		rep, e := chlocalbroker.ExecOnPool(ctx, caller, "introspect", chlocalbroker.ExecRequest{SQL: sql})
		if e != nil {
			return nil, e
		}
		defer func() { _ = rep.Close() }()
		if re := rep.Err(); re != nil {
			return nil, re
		}
		return io.ReadAll(rep)
	})
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	require.NoError(t, introspect.RegisterCatalog(r))
	s := New(Config{Registry: r, Runner: runner}, logger)
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

// TestServer_QueryEndpoint exercises the full self-referential loop: a
// keelson('env') query is rewritten to url() against this server, run by
// clickhouse-local (which fetches /table/env back from this same server),
// and returned as ArrowStream — the play-compatible path (ADR-0094 §SD4).
func TestServer_QueryEndpoint(t *testing.T) {
	s := newQueryServer(t)
	const sql = "SELECT count() AS c FROM keelson('env') FORMAT ArrowStream" // play appends FORMAT
	resp, err := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader(sql))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/vnd.apache.arrow.stream", resp.Header.Get("Content-Type"))

	rdr, err := ipc.NewReader(resp.Body, ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	defer rdr.Release()
	require.True(t, rdr.Next())
	rec := rdr.RecordBatch()
	require.EqualValues(t, 1, rec.NumRows())
	assert.Positive(t, rec.Column(0).(*array.Uint64).Value(0), "env should expose rows")
}

func TestServer_QueryNoRunner503(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	s := New(Config{Registry: r}, zerolog.Nop()) // no Runner
	require.NoError(t, s.Start())
	defer func() { _ = s.Stop(context.Background()) }()
	resp, err := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader("SELECT 1"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestServer_QueryUnknownKeelson400(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	// URLPass rejects the unknown table before the runner is ever reached.
	dummy := RunnerFunc(func(context.Context, string) ([]byte, error) {
		return nil, errors.New("runner must not be called")
	})
	s := New(Config{Registry: r, Runner: dummy}, zerolog.Nop())
	require.NoError(t, s.Start())
	defer func() { _ = s.Stop(context.Background()) }()
	resp, err := http.Post(s.BaseURL()+"/query", "text/plain",
		strings.NewReader("SELECT * FROM keelson('bogus') FORMAT ArrowStream"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(b), "unknown keelson table")
}

// TestServer_QueryRejectsParams: play ships top-level `SET param_*` on the URL
// query string, but the in-process runner cannot bind them — the endpoint must
// reject up front rather than mis-run (ADR-0094 §SD4). Rejected before the
// runner is reached, so no clickhouse-local is needed.
func TestServer_QueryRejectsParams(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	dummy := RunnerFunc(func(context.Context, string) ([]byte, error) {
		return nil, errors.New("runner must not be called when params are present")
	})
	s := New(Config{Registry: r, Runner: dummy}, zerolog.Nop())
	require.NoError(t, s.Start())
	defer func() { _ = s.Stop(context.Background()) }()
	resp, err := http.Post(s.BaseURL()+"/query?param_x=1", "text/plain",
		strings.NewReader("SELECT {x:UInt64} FORMAT ArrowStream"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(b), "parameter binding is not supported")
	assert.Contains(t, string(b), "param_x")
}

// TestServer_QuerySummaryHeader: a successful /query carries a minimal,
// parseable X-ClickHouse-Summary so a client's stats line is not all-zero
// (ADR-0094 §SD4). read_rows/read_bytes are 0 on the in-process path.
func TestServer_QuerySummaryHeader(t *testing.T) {
	s := newQueryServer(t)
	const sql = "SELECT count() AS c FROM keelson('env') FORMAT ArrowStream"
	resp, err := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader(sql))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	hdr := resp.Header.Get("X-ClickHouse-Summary")
	require.NotEmpty(t, hdr, "expected an X-ClickHouse-Summary header")
	var kv map[string]string
	require.NoError(t, json.Unmarshal([]byte(hdr), &kv), "summary header must be valid JSON: %q", hdr)
	rb, err := strconv.Atoi(kv["result_bytes"])
	require.NoError(t, err, "result_bytes must be numeric: %q", kv["result_bytes"])
	assert.Positive(t, rb, "result_bytes should reflect the ArrowStream size")
	assert.Contains(t, kv, "elapsed_ns")
}

// TestServer_QueryAppliesPreExecutePasses: SQL posted to /query goes
// through the registered pre-execute rewrites before the keelson-url
// rewrite and the runner (ADR-0108 §SD6). A stub runner captures the SQL,
// so no clickhouse-local is needed; the pass registry is injected so the
// process-global passreg.Default stays untouched.
func TestServer_QueryAppliesPreExecutePasses(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	var gotSQL string
	runner := RunnerFunc(func(_ context.Context, sql string) ([]byte, error) {
		gotSQL = sql
		return []byte("ok"), nil
	})
	pr := passreg.NewRegistry()
	require.NoError(t, pr.Register(passreg.Entry{
		Pass: nanopass.LiftBodyPass("TestRewrite", func(sql string) (string, error) {
			return strings.Replace(sql, "SELECT 1", "SELECT 2", 1), nil
		}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody}),
		Stage: passreg.StagePreExecute,
	}))
	s := New(Config{Registry: r, Runner: runner, Passes: pr}, zerolog.Nop())
	require.NoError(t, s.Start())
	defer func() { _ = s.Stop(context.Background()) }()

	resp, err := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader("SELECT 1"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "SELECT 2", gotSQL)
}

// TestServer_QueryFailingPreExecutePassFallsBack: a broken registered pass
// is skipped best-effort — the /query SQL from before it still runs.
func TestServer_QueryFailingPreExecutePassFallsBack(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r))
	var gotSQL string
	runner := RunnerFunc(func(_ context.Context, sql string) ([]byte, error) {
		gotSQL = sql
		return []byte("ok"), nil
	})
	pr := passreg.NewRegistry()
	require.NoError(t, pr.Register(passreg.Entry{
		Pass: nanopass.LiftBodyPass("Broken", func(string) (string, error) {
			return "", errors.New("boom")
		}, nanopass.PassProperties{Reads: nanopass.RegionBody, Writes: nanopass.RegionBody}),
		Stage: passreg.StagePreExecute,
	}))
	s := New(Config{Registry: r, Runner: runner, Passes: pr}, zerolog.Nop())
	require.NoError(t, s.Start())
	defer func() { _ = s.Stop(context.Background()) }()

	resp, err := http.Post(s.BaseURL()+"/query", "text/plain", strings.NewReader("SELECT 1"))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "SELECT 1", gotSQL)
}
