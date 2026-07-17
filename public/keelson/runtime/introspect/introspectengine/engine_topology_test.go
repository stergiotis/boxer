package introspectengine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// TestQuery_TopologyTables drives the whole ADR-0126 §SD5 chain the way
// a user does: a bundle published on the metric plane becomes rows the
// SQL surface serves — keelson('procs') filtered by component, and a
// procs⋈sockets join walking a listener back to its component.
func TestQuery_TopologyTables(t *testing.T) {
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
		Bus: bus.NewClient("test.topo.holder", []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub, Reason: "test"},
		}),
		Log: logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })

	reg := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(reg))
	require.NoError(t, providers.RegisterTopology(reg, holder))

	payload, err := sysmetricsbus.NewCBORCodec().Encode(&sysmsnap.BundleSnapshot{
		SampledAtUnixMs: 42_000,
		Procs: []sysmsnap.ProcInfo{
			{PID: 4711, PPID: 1, Name: "carrier", Component: "imzero2-demo", CgroupUnit: "imzero2-demo.service", State: 'S'},
			{PID: 9000, PPID: 1, Name: "plain", State: 'R'},
		},
		Sockets: &sysmsnap.SocketsSnapshot{
			CollectedAtUnixMs: 41_500,
			Sockets: []sysmsnap.SocketInfo{
				{Proto: sysmsnap.SocketProtoTCP, Addr: "127.0.0.1", Port: 8089, Inode: 777, PID: 4711},
			},
		},
	})
	require.NoError(t, err)
	pub := bus.NewClient("test.topo.scraper", []app.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub, Reason: "test"},
	})
	require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject("box-a"), payload))

	caller := bus.NewClient("test.topo.engine", []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"},
	})
	e, err := New(Config{Registry: reg, Bus: caller}, logger)
	require.NoError(t, err)

	// The component filter — the first drift-style query.
	body, _, err := e.Query(context.Background(),
		"SELECT pid FROM keelson('procs') WHERE component = 'imzero2-demo'", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "4711", strings.TrimSpace(string(body)))

	// The listener walk: socket → pid → component, across two tables.
	body, _, err = e.Query(context.Background(),
		"SELECT p.component FROM keelson('sockets') AS s INNER JOIN keelson('procs') AS p ON s.pid = p.pid AND s.host = p.host WHERE s.port = 8089",
		"TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "imzero2-demo", strings.TrimSpace(string(body)))

	// The declared inventory is queryable beside the observed tables.
	body, _, err = e.Query(context.Background(),
		"SELECT count() FROM keelson('components') WHERE has(needs, 'imzero2-demo')", "TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(body)), "caddy needs imzero2-demo")

	// THE drift query (ADR-0126 §SD1): declared-but-not-observed
	// components as a single-table GROUP BY over origins. Only
	// imzero2-demo runs in the fixture, so every other registry
	// component is drift — spot-check one and the running one's absence.
	body, _, err = e.Query(context.Background(),
		"SELECT key FROM keelson('topology_nodes') WHERE kind = 'component' GROUP BY key HAVING NOT has(groupArray(origin), 'observed') ORDER BY key",
		"TabSeparated")
	require.NoError(t, err)
	drift := strings.TrimSpace(string(body))
	assert.Contains(t, drift, "component:caddy")
	assert.NotContains(t, drift, "component:imzero2-demo")

	// The socket-owner walk over the graph rows alone: listener edge →
	// containment edge, no typed tables involved.
	body, _, err = e.Query(context.Background(),
		"SELECT c.dst_key FROM keelson('topology_edges') AS l INNER JOIN keelson('topology_edges') AS c ON l.src_key = c.src_key AND l.host = c.host WHERE l.edge_kind = 'proc-listens' AND l.dst_key = 'sock:tcp/127.0.0.1:8089' AND c.edge_kind = 'proc-in-component'",
		"TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "component:imzero2-demo", strings.TrimSpace(string(body)))

	// The dependency closure from the howto (WITH RECURSIVE over
	// component-needs): caddy's closure is exactly the carrier.
	body, _, err = e.Query(context.Background(),
		"WITH RECURSIVE closure AS ("+
			" SELECT dst_key FROM keelson('topology_edges') WHERE edge_kind = 'component-needs' AND src_key = 'component:caddy'"+
			" UNION ALL"+
			" SELECT e.dst_key FROM keelson('topology_edges') AS e INNER JOIN closure AS c ON e.src_key = c.dst_key WHERE e.edge_kind = 'component-needs'"+
			") SELECT DISTINCT dst_key FROM closure",
		"TabSeparated")
	require.NoError(t, err)
	assert.Equal(t, "component:imzero2-demo", strings.TrimSpace(string(body)))
}
