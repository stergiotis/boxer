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
	require.Len(t, defs, 4)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 4)
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
	assert.Equal(t, []string{"pprof_cpu"}, bySlug["profile-flame"].Datasets)
	assert.Equal(t, []string{"pprof_heap"}, bySlug["profile-heap"].Datasets)

	masterDetail := []TabSel{{ID: "table"}, {ID: "detail"}}
	assert.Equal(t, masterDetail, bySlug["profile-top"].Tabs)
	assert.Equal(t, masterDetail, bySlug["profile-heap"].Tabs)
	assert.Equal(t, []TabSel{{ID: "network"}, {ID: "table"}, {ID: "detail"}}, bySlug["profile-callgraph"].Tabs)
	assert.Equal(t, []TabSel{{ID: "icicle"}, {ID: "table"}, {ID: "detail"}}, bySlug["profile-flame"].Tabs)

	// The graph lens feeds the network panel through the convention-named
	// top-level CTEs (ADR-0129).
	assert.Contains(t, bySlug["profile-callgraph"].SQL, "edges AS (")
	assert.Contains(t, bySlug["profile-callgraph"].SQL, "vertices AS (")

	// The flame lens is a projection into the ADR-0160 folded contract: a
	// list-typed `stack` and each stack's OWN `value`, which is what the
	// converter already emits. A `stack` that stopped being an array — or a
	// GROUP BY that rolled paths into their prefixes — would draw nothing.
	assert.Contains(t, bySlug["profile-flame"].SQL, "stack,")
	assert.Contains(t, bySlug["profile-flame"].SQL, "AS value")
	assert.NotContains(t, bySlug["profile-flame"].SQL, "GROUP BY")
}

// TestMintPprofBook mints the book beside its siblings, guarding slug
// collisions across the four embedded corpora, and pins the resolve
// capability a dataset-declaring applet carries.
func TestMintPprofBook(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "sqlapplet", fsys: help.MustSub(bookFS, "book"), topics: []app.TopicT{app.TopicRuntime}},
		{id: "topology", fsys: help.MustSub(booktopoFS, "booktopo"), topics: []app.TopicT{app.TopicTopology}},
		{id: "godep", fsys: help.MustSub(bookgodepFS, "bookgodep"), topics: []app.TopicT{app.TopicCode}},
		{id: "pprof", fsys: help.MustSub(bookpprofFS, "bookpprof"), topics: []app.TopicT{app.TopicObservability}},
	})
	require.Empty(t, errs)
	assert.Equal(t, 18, minted)

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
	bindings, unresolved := resolveDatasetAliases(appletBus, logger, []string{"pprof_cpu", "pprof_heap"})
	require.Len(t, bindings, 2)
	require.Empty(t, unresolved)

	query := func(sql string, format string) (out string) {
		t.Helper()
		resp, perr := http.Post(srv.BaseURL()+"/query", "text/plain", strings.NewReader(sql+" FORMAT "+format))
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
		out := query(sql, "TabSeparated")
		assert.NotEmpty(t, out, "%s: the sink returned no rows", slug)

		// The flame lens is resolved from the Arrow SCHEMA (ADR-0160 §SD9),
		// not from the rows, so returning rows is not evidence it draws: a
		// `stack` that stopped being a list, or a `value` that came back as
		// text, rejects with data on the wire. Pin the two types the panel
		// keys on — the header carries them.
		if slug == "profile-flame" {
			head := query(sql, "TabSeparatedWithNamesAndTypes")
			lines := strings.SplitN(head, "\n", 3)
			require.Len(t, lines, 3, "profile-flame: no names+types header")
			assert.Equal(t, "stack\tvalue\tunit", lines[0])
			assert.Equal(t, "Array(String)\tFloat64\tString", lines[1],
				"profile-flame: the folded contract needs a list `stack` and a numeric `value`")
		}
	}

	// A miss binds nothing and fails nothing, and reports the alias back so
	// the open window can keep trying: the applet-open path over an alias
	// never captured.
	missBindings, missUnresolved := resolveDatasetAliases(appletBus, logger, []string{"pprof_goroutine"})
	assert.Empty(t, missBindings)
	assert.Equal(t, []string{"pprof_goroutine"}, missUnresolved)

	// And the retry path closes it: a rebinder over that alias picks up the
	// dataset published after open, without the window being reopened.
	reb := newDatasetRebinder(appletBus, logger, "hint.", missUnresolved)
	require.NotNil(t, reb)
	// Any instance will do as the bind target — the rebinder's contract is
	// with PlayApp.BindDataset, not with this buffer's aliases.
	inner, err := NewEmbedded(pprofDefsBySlug(t)["profile-top"], EmbedConfig{
		StampAppId: "test.rebind", Bus: appletBus, Log: logger, EndpointURL: srv.BaseURL() + "/query",
	})
	require.NoError(t, err)

	var goroutineBuf bytes.Buffer
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(&goroutineBuf, 0))
	publish("goroutine", goroutineBuf.Bytes())

	var boundOK bool
	for range 100 {
		// Drive the retries at test speed; the interval is not what this
		// test is about.
		reb.mu.Lock()
		reb.nextAt = time.Time{}
		reb.mu.Unlock()
		bound, _, _, done := reb.sync(inner.BindDataset)
		if bound && done {
			boundOK = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.True(t, boundOK, "the rebinder never picked up the after-open publish")
}
