package sqlapplet

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
	"github.com/stergiotis/boxer/public/observability/profiling/pprofarrow"
)

// pprofDefsBySlug parses the embedded profile book and indexes it by slug.
func pprofDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("pprof", help.MustSub(bookpprofFS, "bookpprof"))
	require.Empty(t, errs)
	require.Len(t, defs, 3)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 3)
	return bySlug
}

// TestPprofBookCorpus is the ADR-0132 §SD6 gate over the profile book.
func TestPprofBookCorpus(t *testing.T) {
	bySlug := pprofDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.NotEmpty(t, d.Icon, slug)
		// Every knob is prelude-bound: the buffer runs on mount, and the
		// missing-dataset case surfaces as a readable error, not a hang.
		assert.False(t, d.HasUnboundSlots, "%s: every knob is prelude-bound", slug)
	}

	assert.Equal(t, []string{"pprof_cpu"}, bySlug["profile-top"].Datasets)
	assert.Equal(t, []string{"pprof_cpu"}, bySlug["profile-callgraph"].Datasets)
	assert.Equal(t, []string{"pprof_heap"}, bySlug["profile-heap"].Datasets)

	masterDetail := []TabSel{{ID: "table"}, {ID: "detail"}}
	assert.Equal(t, masterDetail, bySlug["profile-top"].Tabs)
	assert.Equal(t, masterDetail, bySlug["profile-heap"].Tabs)
	assert.Equal(t, []TabSel{{ID: "network"}, {ID: "table"}, {ID: "detail"}}, bySlug["profile-callgraph"].Tabs)

	// The graph lens feeds the network panel through the convention-named
	// top-level CTEs (ADR-0129).
	assert.Contains(t, bySlug["profile-callgraph"].SQL, "edges AS (")
	assert.Contains(t, bySlug["profile-callgraph"].SQL, "vertices AS (")
}

// TestMintPprofBook mints the book beside its siblings, guarding slug
// collisions across the four embedded corpora, and pins the resolve
// capability a dataset-declaring applet carries.
func TestMintPprofBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "sqlapplet", fsys: help.MustSub(bookFS, "book")},
		{id: "topology", fsys: help.MustSub(booktopoFS, "booktopo")},
		{id: "godep", fsys: help.MustSub(bookgodepFS, "bookgodep")},
		{id: "pprof", fsys: help.MustSub(bookpprofFS, "bookpprof")},
	})
	require.Empty(t, errs)
	assert.Equal(t, 16, minted)

	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "profile-top"))
	require.True(t, ok)
	var hasResolve bool
	for _, c := range m.Caps {
		if c.Pattern == adhocdata.SubjectResolve && c.Direction == app.CapDirectionPub {
			hasResolve = true
		}
	}
	assert.True(t, hasResolve, "a dataset-declaring applet needs Pub on adhoc.resolve")

	// A dataset-less applet keeps the two-cap escape-hatch surface.
	m, ok = reg.LookupManifest(app.AppIdT(appletIdPrefix + "runtime-apps"))
	require.True(t, ok)
	for _, c := range m.Caps {
		assert.NotEqual(t, adhocdata.SubjectResolve, c.Pattern, "runtime-apps declares no datasets")
	}
}

// burnCPU keeps the process busy so a short CPU capture carries samples.
func burnCPU(d time.Duration) (acc int) {
	until := time.Now().Add(d)
	for time.Now().Before(until) {
		for i := range 1 << 12 {
			acc += i * i
		}
	}
	return
}

var bookpprofSink [][]byte

// TestPprofBookQueriesExecute is the live half of the corpus gate: it
// captures real profiles of this test process, publishes them through the
// adhoc capability service, resolves the books' declared aliases exactly
// the way appletApp.Mount does, and runs every buffer — SET prelude
// included — through the production /query endpoint with the aliases bound.
func TestPprofBookQueriesExecute(t *testing.T) {
	if _, err := exec.LookPath(chlocalpool.DefaultBinaryPath); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(60 * time.Second)

	broker, err := chlocalbroker.NewService(bus, chlocalpool.Config{
		BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 3, SpawnConcurrency: 1,
	}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = broker.Stop(ctx)
	})

	reg := introspect.NewRegistry()
	svc, err := adhocdata.NewService(adhocdata.Config{
		Bus: bus, Registry: reg, Keys: broker.KeyStore(), Dir: t.TempDir(), Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	runner := introspecthttp.RunnerFunc(func(ctx context.Context, sql string, params map[string]string) ([]byte, error) {
		rep, e := chlocalbroker.ExecOnPool(ctx, bus.NewClient("test.bookpprof.exec", []app.SubjectFilter{
			{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
		}), "introspect", chlocalbroker.ExecRequest{SQL: sql, Params: params})
		if e != nil {
			return nil, e
		}
		defer func() { _ = rep.Close() }()
		if re := rep.Err(); re != nil {
			return nil, re
		}
		return io.ReadAll(rep)
	})
	srv := introspecthttp.New(introspecthttp.Config{Registry: reg, Runner: runner, Decryptor: broker}, logger)
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// Capture and publish under the aliases the books declare — the imzrt
	// Profiles flow, minus the GUI.
	publish := func(kind string, raw []byte) {
		t.Helper()
		conv, cErr := pprofarrow.Convert(bytes.NewReader(raw), pprofarrow.WithKindHint(kind))
		require.NoError(t, cErr, kind)
		require.Positive(t, conv.Rows, kind)
		_, pErr := svc.Publish(adhocdata.PublishInput{
			Alias: "pprof_" + kind, Publisher: "test", ArrowIPCStream: conv.IPCStream,
		})
		require.NoError(t, pErr, kind)
	}

	var cpuBuf bytes.Buffer
	require.NoError(t, pprof.StartCPUProfile(&cpuBuf))
	bookpprofSink = append(bookpprofSink, make([]byte, 1<<20))
	_ = burnCPU(400 * time.Millisecond)
	pprof.StopCPUProfile()
	publish("cpu", cpuBuf.Bytes())

	for range 2048 {
		bookpprofSink = append(bookpprofSink, make([]byte, 2048))
	}
	runtime.GC()
	var heapBuf bytes.Buffer
	require.NoError(t, pprof.Lookup("heap").WriteTo(&heapBuf, 0))
	publish("heap", heapBuf.Bytes())

	// Resolve the aliases the way appletApp.Mount does — through the bus
	// with exactly the capability a minted applet declares.
	appletBus := bus.NewClient("test.bookpprof.applet", []app.SubjectFilter{
		{Pattern: adhocdata.SubjectResolve, Direction: app.CapDirectionPub, Reason: "test"},
	})
	bindings := resolveDatasetAliases(appletBus, logger, []string{"pprof_cpu", "pprof_heap"})
	require.Len(t, bindings, 2)

	query := func(sql string) (out string) {
		t.Helper()
		resp, perr := http.Post(srv.BaseURL()+"/query", "text/plain", strings.NewReader(sql+" FORMAT TabSeparated"))
		require.NoError(t, perr)
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		require.Equalf(t, http.StatusOK, resp.StatusCode, "body: %s", raw)
		return strings.TrimSpace(string(raw))
	}

	for slug, d := range pprofDefsBySlug(t) {
		sql := d.SQL
		for alias, handle := range bindings {
			sql = strings.ReplaceAll(sql, "keelson('"+alias+"')", "keelson('"+handle+"')")
		}
		out := query(sql)
		assert.NotEmpty(t, out, "%s: the sink returned no rows", slug)
	}

	// A miss binds nothing and fails nothing: the applet-open path over an
	// alias never captured.
	assert.Empty(t, resolveDatasetAliases(appletBus, logger, []string{"pprof_goroutine"}))
}
