package providers

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// col resolves a column by name on rec (the schema is projection-
// dependent, so tests never index positionally).
func col(t *testing.T, rec arrow.RecordBatch, name string) arrow.Array {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	return rec.Column(idx[0])
}

func TestComponentsProvider_ServesRegistry(t *testing.T) {
	p := componentsProvider{}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()

	registry := topo.Registry()
	require.EqualValues(t, len(registry), rec.NumRows())

	tokens := col(t, rec, "token").(*array.String)
	needs := col(t, rec, "needs").(*array.List)
	caddyRow := -1
	for i := 0; i < tokens.Len(); i++ {
		require.Equal(t, registry[i].Token, tokens.Value(i), "sorted registry order")
		if tokens.Value(i) == topo.Caddy.Token {
			caddyRow = i
		}
	}
	require.GreaterOrEqual(t, caddyRow, 0)

	// caddy declares component-needs -> imzero2-demo (ADR-0126 §SD2).
	start, end := needs.ValueOffsets(caddyRow)
	values := needs.ListValues().(*array.String)
	found := false
	for j := start; j < end; j++ {
		if values.Value(int(j)) == topo.ImZero2Demo.Token {
			found = true
		}
	}
	assert.True(t, found, "caddy needs imzero2-demo")
}

// TestTopologyProviders_PlaneFed drives the whole SD5 chain: a bundle
// published on the metric plane lands, via the latest-holder, as rows of
// keelson.procs and keelson.sockets.
func TestTopologyProviders_PlaneFed(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	holder, err := sysmetricsbus.StartLatestHolder(sysmetricsbus.LatestHolderOptions{
		Bus: bus.NewClient("holder", []runtimeapp.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: runtimeapp.CapDirectionSub},
		}),
		Log: zerolog.Nop(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Close() })

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterTopology(reg, holder))

	procs, ok := reg.Lookup("procs")
	require.True(t, ok)
	sockets, ok := reg.Lookup("sockets")
	require.True(t, ok)

	// Before any bundle arrives both tables are empty, not absent.
	rec, err := procs.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	require.EqualValues(t, 0, rec.NumRows())
	rec.Release()

	payload, err := sysmetricsbus.NewCBORCodec().Encode(&sysmsnap.BundleSnapshot{
		SampledAtUnixMs: 42_000,
		Procs: []sysmsnap.ProcInfo{
			{PID: 4711, PPID: 1, Name: "carrier", Cmd: "main_go imzero2 demo",
				Component: "imzero2-demo", CgroupUnit: "imzero2-demo.service",
				State: 'S', UID: 1000, RSSBytes: 1 << 20, CPUPercent: 12.5},
			{PID: 9000, PPID: 1, Name: "plain", State: 'R'},
		},
		Sockets: &sysmsnap.SocketsSnapshot{
			CollectedAtUnixMs: 41_500,
			Sockets: []sysmsnap.SocketInfo{
				{Proto: sysmsnap.SocketProtoTCP, Addr: "127.0.0.1", Port: 8089, Inode: 777, UID: 1000, PID: 4711},
			},
		},
	})
	require.NoError(t, err)
	pub := bus.NewClient("scraper", []runtimeapp.SubjectFilter{
		{Pattern: sysmetricsbus.SubjectWildcard, Direction: runtimeapp.CapDirectionPub},
	})
	require.NoError(t, pub.Publish(sysmetricsbus.BundleSubject("box-a"), payload))

	rec, err = procs.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "box-a", col(t, rec, "host").(*array.String).Value(0))
	assert.Equal(t, int64(4711), col(t, rec, "pid").(*array.Int64).Value(0))
	assert.Equal(t, "imzero2-demo", col(t, rec, "component").(*array.String).Value(0))
	assert.Equal(t, "imzero2-demo.service", col(t, rec, "cgroup_unit").(*array.String).Value(0))
	assert.Equal(t, "S", col(t, rec, "state").(*array.String).Value(0))
	assert.InDelta(t, 12.5, col(t, rec, "cpu_percent").(*array.Float64).Value(0), 1e-9)
	assert.Equal(t, uint64(1<<20), col(t, rec, "rss_bytes").(*array.Uint64).Value(0))
	assert.Equal(t, int64(42_000), col(t, rec, "sampled_at_unix_ms").(*array.Int64).Value(0))
	assert.Equal(t, "", col(t, rec, "component").(*array.String).Value(1))

	srec, err := sockets.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer srec.Release()
	require.EqualValues(t, 1, srec.NumRows())
	assert.Equal(t, "box-a", col(t, srec, "host").(*array.String).Value(0))
	assert.Equal(t, "tcp", col(t, srec, "proto").(*array.String).Value(0))
	assert.Equal(t, "127.0.0.1", col(t, srec, "addr").(*array.String).Value(0))
	assert.Equal(t, int32(8089), col(t, srec, "port").(*array.Int32).Value(0))
	assert.Equal(t, int64(4711), col(t, srec, "pid").(*array.Int64).Value(0))
	assert.Equal(t, uint64(777), col(t, srec, "inode").(*array.Uint64).Value(0))
	assert.Equal(t, int64(41_500), col(t, srec, "collected_at_unix_ms").(*array.Int64).Value(0))

	// The graph projection: the marked component's key carries BOTH
	// origins (registry-declared + mark-observed) — the drift join's
	// substrate — and the listener edge attributes the socket.
	nodes, ok := reg.Lookup("topology_nodes")
	require.True(t, ok)
	nrec, err := nodes.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer nrec.Release()
	kinds := col(t, nrec, "kind").(*array.String)
	keys := col(t, nrec, "key").(*array.String)
	origins := col(t, nrec, "origin").(*array.String)
	var sawDeclared, sawObserved bool
	for i := 0; i < int(nrec.NumRows()); i++ {
		if kinds.Value(i) == "component" && keys.Value(i) == "component:imzero2-demo" {
			switch origins.Value(i) {
			case "declared":
				sawDeclared = true
			case "observed":
				sawObserved = true
			}
		}
	}
	assert.True(t, sawDeclared, "component:imzero2-demo declared row")
	assert.True(t, sawObserved, "component:imzero2-demo observed row")

	edges, ok := reg.Lookup("topology_edges")
	require.True(t, ok)
	erec, err := edges.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer erec.Release()
	ekinds := col(t, erec, "edge_kind").(*array.String)
	srcs := col(t, erec, "src_key").(*array.String)
	dsts := col(t, erec, "dst_key").(*array.String)
	foundListens := false
	for i := 0; i < int(erec.NumRows()); i++ {
		if ekinds.Value(i) == "proc-listens" && srcs.Value(i) == "proc:4711" && dsts.Value(i) == "sock:tcp/127.0.0.1:8089" {
			foundListens = true
		}
	}
	assert.True(t, foundListens, "proc-listens edge for the attributed socket")
}
