package introspecthttp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
