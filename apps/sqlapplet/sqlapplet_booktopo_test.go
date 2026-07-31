package sqlapplet

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// topologyDefsBySlug parses the embedded topology book and indexes the
// applet definitions by slug.
func topologyDefsBySlug(t *testing.T) map[string]*AppletDef {
	t.Helper()
	defs, errs := ParseBook("topology", help.MustSub(booktopoFS, "booktopo"))
	require.Empty(t, errs)
	require.Len(t, defs, 6)
	bySlug := make(map[string]*AppletDef, len(defs))
	for _, d := range defs {
		bySlug[d.Slug] = d
	}
	require.Len(t, bySlug, 6)
	return bySlug
}

// TestTopologyBookCorpus is the ADR-0132 §SD6 hard gate over the embedded
// topology book: every doc parses, classifies as a read, pins the
// introspection endpoint, and keeps its parameters prelude-bound.
func TestTopologyBookCorpus(t *testing.T) {
	bySlug := topologyDefsBySlug(t)
	for slug, d := range bySlug {
		assert.Equal(t, EndpointIntrospection, d.Endpoint, slug)
		assert.Equal(t, analysis.QuerySecurityRead, d.Class, "%s: keelson('…') classifies as a local read", slug)
		assert.False(t, d.HasUnboundSlots, "%s: params are prelude-bound — widgets, not signals", slug)
		assert.NotEmpty(t, d.Icon, slug)
	}

	graphTabs := []TabSel{{ID: "network"}, {ID: "table"}}
	tableTabs := []TabSel{{ID: "table"}, {ID: "detail"}}
	assert.Equal(t, graphTabs, bySlug["topology-map"].Tabs)
	assert.Equal(t, graphTabs, bySlug["component-deps"].Tabs)
	assert.Equal(t, tableTabs, bySlug["topology-drift"].Tabs)
	assert.Equal(t, tableTabs, bySlug["socket-owners"].Tabs)
	assert.Equal(t, tableTabs, bySlug["component-procs"].Tabs)
	assert.Equal(t, []TabSel{{ID: "table"}}, bySlug["plane-staleness"].Tabs)
}

// TestMintAllEmbeddedBooks mints both embedded books together — the
// composition the shell's init-time registrations produce — guarding
// cross-book slug collisions.
func TestMintAllEmbeddedBooks(t *testing.T) {
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{
		{id: "sqlapplet", fsys: help.MustSub(bookFS, "book")},
		{id: "topology", fsys: help.MustSub(booktopoFS, "booktopo")},
	})
	require.Empty(t, errs)
	assert.Equal(t, 9, minted)
	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "topology-map"))
	require.True(t, ok)
	assert.Equal(t, "Topology map", m.Display)
}

// TestTopologyBookQueriesExecute runs every topology-book buffer verbatim —
// SET prelude included, the multi-statement form ADR-0133 §SD2 verified
// against clickhouse-local — through the introspect engine, over the same
// fixture bundle the engine's own topology test publishes. This is the
// live half of the corpus gate: the buffers not only parse, they answer.
func TestTopologyBookQueriesExecute(t *testing.T) {
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

	holder, err := sysmetricsbus.StartLatestHolder(sysmetricsbus.LatestHolderOptions{
		Bus: bus.NewClient("test.booktopo.holder", []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub, Reason: "test"},
		}),
		Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))
	require.NoError(t, providers.RegisterTopology(reg, holder))

	// Beside the engine test's marked carrier and unmarked plain proc: a
	// mark no registry entry declares (drift's observed-only direction) and
	// a pid-0 listener — the fd table the collector could not read, e.g. a
	// root-owned or containerised daemon (the map's `unattributed` node).
	payload, err := sysmetricsbus.NewCBORCodec().Encode(&sysmsnap.BundleSnapshot{
		SampledAtUnixMs: 42_000,
		Procs: []sysmsnap.ProcInfo{
			{PID: 4711, PPID: 1, Name: "carrier", Component: "imzero2-demo", CgroupUnit: "imzero2-demo.service", State: 'S'},
			{PID: 9000, PPID: 1, Name: "plain", State: 'R'},
			{PID: 9100, PPID: 1, Name: "rogue", Component: "rogue-svc", State: 'S'},
		},
		Sockets: &sysmsnap.SocketsSnapshot{
			CollectedAtUnixMs: 41_500,
			Sockets: []sysmsnap.SocketInfo{
				{Proto: sysmsnap.SocketProtoTCP, Addr: "127.0.0.1", Port: 8089, Inode: 777, PID: 4711},
				{Proto: sysmsnap.SocketProtoTCP, Addr: "0.0.0.0", Port: 4222, Inode: 900, PID: 0},
			},
		},
	})
	require.NoError(t, err)
	pub := bus.NewClient("test.booktopo.scraper", []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub, Reason: "test"},
	})
	require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject("box-a"), payload))

	caller := bus.NewClient("test.booktopo.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := introspectengine.New(introspectengine.Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	bySlug := topologyDefsBySlug(t)
	run := func(slug string) string {
		t.Helper()
		def := bySlug[slug]
		require.NotNil(t, def, slug)
		body, _, qerr := e.Query(context.Background(), def.SQL, "TabSeparated")
		require.NoError(t, qerr, slug)
		return string(body)
	}

	// The map: declared needs edges plus the observed listener walked to
	// its component. The pid-0 socket must NOT surface as a listener edge.
	out := run("topology-map")
	assert.Contains(t, out, "needs")
	assert.Contains(t, out, "sock:tcp/127.0.0.1:8089")
	assert.NotContains(t, out, "4222")

	// The map's vertices carry the liveness join: same CTEs, sink swapped
	// to probe what the Network tab's vertices lane will read.
	mapDef := bySlug["topology-map"]
	require.NotNil(t, mapDef)
	vsql := strings.Replace(mapDef.SQL, "SELECT * FROM edges",
		"SELECT label, `group` FROM vertices ORDER BY id", 1)
	require.NotEqual(t, mapDef.SQL, vsql, "vertices probe: sink replacement missed")
	vout, _, verr := e.Query(context.Background(), vsql, "TabSeparated")
	require.NoError(t, verr)
	assert.Contains(t, string(vout), "imzero2-demo ·1\trunning")
	assert.Contains(t, string(vout), "caddy ·0\tabsent")
	assert.Contains(t, string(vout), "rogue-svc ·1\tundeclared")
	assert.Contains(t, string(vout), "unattributed ·1\tunattributed")

	// Drift: caddy is declared with no live mark, imzero2-demo runs as
	// declared and carries the observing host, the rogue mark is the
	// observed-only direction.
	out = run("topology-drift")
	assert.Contains(t, out, "declared-only")
	assert.Contains(t, out, "both")
	assert.Contains(t, out, "component:caddy")
	assert.Contains(t, out, "box-a")
	assert.Contains(t, out, "observed-only")
	assert.Contains(t, out, "component:rogue-svc")

	// The default port filter (0) keeps every listener — attributed and
	// pid-0 alike (the LEFT JOIN keeps the row, process columns empty).
	out = run("socket-owners")
	assert.Contains(t, out, "8089")
	assert.Contains(t, out, "imzero2-demo")
	assert.Contains(t, out, "4222")

	// The default LIKE pattern keeps marked processes only.
	out = run("component-procs")
	assert.Contains(t, out, "carrier")
	assert.Contains(t, out, "rogue-svc")
	assert.NotContains(t, out, "plain")

	// caddy's declared closure is exactly the carrier component.
	out = run("component-deps")
	assert.Equal(t, "component:imzero2-demo", strings.TrimSpace(out))

	// Both observed domains report their clocks for the fixture host.
	out = run("plane-staleness")
	assert.Contains(t, out, "procs")
	assert.Contains(t, out, "sockets")
	assert.Contains(t, out, "box-a")
}
