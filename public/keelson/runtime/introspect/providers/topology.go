// topology — components, procs, sockets (ADR-0126 §SD5): the typed
// observed-topology tables. keelson.components is the compiled-in
// declared inventory; keelson.procs and keelson.sockets flatten the
// latest metric-plane bundle per host, so they register only where a
// plane consumer exists (RegisterTopology), unlike the static set.

package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// RegisterTopology registers the plane-fed observed-topology providers
// (procs, sockets) reading holder's latest snapshots. Call from a host
// that stands up a metric-plane consumer (ADR-0126 §SD5); rows are
// empty until a scraper publishes.
func RegisterTopology(r *introspect.Registry, holder *sysmetricsbus.LatestHolder) (err error) {
	for _, p := range []introspect.Provider{
		procsProvider{holder: holder}, socketsProvider{holder: holder},
	} {
		if err = r.Register(p); err != nil {
			return
		}
	}
	return
}

// --- components (topo registry) ----------------------------------------------

// componentsProvider exposes the declared component inventory
// (keelson/runtime/topo) as keelson.components. Compiled-in, so Static.
type componentsProvider struct{}

func (componentsProvider) Name() string                         { return "components" }
func (componentsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (componentsProvider) Schema() *arrow.Schema                { return componentsTable(nil).Schema() }

func (componentsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	cs := topo.Registry() // sorted by Token
	return componentsTable(cs).Build(proj, len(cs)), nil
}

func componentsTable(cs []*topo.Component) *introspect.Table {
	return introspect.NewTable().
		String("token", func(i int) string { return cs[i].Token }).
		String("role", func(i int) string { return cs[i].Role }).
		StringList("needs", func(i int) []string { return cs[i].Needs })
}

// --- procs (metric plane, proc domain) ---------------------------------------

// procsProvider exposes the latest per-host process table as
// keelson.procs — deferred in ADR-0094 §SD8 v2, landed by ADR-0126
// because the proc↔component join needs it.
type procsProvider struct{ holder *sysmetricsbus.LatestHolder }

func (procsProvider) Name() string                         { return "procs" }
func (procsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (procsProvider) Schema() *arrow.Schema                { return procsTable(nil).Schema() }

// procRow flattens (host bundle, proc index) so the Table getters index
// one flat slice.
type procRow struct {
	host       string
	sampledAt  int64
	receivedAt int64
	info       *sysmsnap.ProcInfo
}

func (p procsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []procRow
	for _, hs := range p.holder.Hosts() {
		for i := range hs.Snap.Procs {
			rows = append(rows, procRow{
				host:       hs.Host,
				sampledAt:  hs.Snap.SampledAtUnixMs,
				receivedAt: hs.ReceivedAtUnixMs,
				info:       &hs.Snap.Procs[i],
			})
		}
	}
	return procsTable(rows).Build(proj, len(rows)), nil
}

func procsTable(rows []procRow) *introspect.Table {
	return introspect.NewTable().
		String("host", func(i int) string { return rows[i].host }).
		Int64("pid", func(i int) int64 { return int64(rows[i].info.PID) }).
		Int64("ppid", func(i int) int64 { return int64(rows[i].info.PPID) }).
		String("name", func(i int) string { return rows[i].info.Name }).
		String("cmd", func(i int) string { return rows[i].info.Cmd }).
		String("component", func(i int) string { return rows[i].info.Component }).
		String("cgroup_unit", func(i int) string { return rows[i].info.CgroupUnit }).
		String("state", func(i int) string { return procState(rows[i].info.State) }).
		Int64("uid", func(i int) int64 { return int64(rows[i].info.UID) }).
		Int64("gid", func(i int) int64 { return int64(rows[i].info.GID) }).
		String("user", func(i int) string { return rows[i].info.User }).
		Int64("started_at_unix_ms", func(i int) int64 { return rows[i].info.StartedAtUnixMs }).
		Float64("cpu_percent", func(i int) float64 { return float64(rows[i].info.CPUPercent) }).
		Uint64("rss_bytes", func(i int) uint64 { return rows[i].info.RSSBytes }).
		Uint64("vm_size_bytes", func(i int) uint64 { return rows[i].info.VMSizeBytes }).
		Int32("num_threads", func(i int) int32 { return rows[i].info.NumThreads }).
		Bool("kernel_thread", func(i int) bool { return rows[i].info.KernelThread }).
		Int64("sampled_at_unix_ms", func(i int) int64 { return rows[i].sampledAt }).
		Int64("received_at_unix_ms", func(i int) int64 { return rows[i].receivedAt })
}

func procState(b byte) (s string) {
	if b == 0 {
		return ""
	}
	return string(rune(b))
}

// --- sockets (metric plane, sockets domain) ----------------------------------

// socketsProvider exposes the latest per-host listener table as
// keelson.sockets (ADR-0126 §SD5).
type socketsProvider struct{ holder *sysmetricsbus.LatestHolder }

func (socketsProvider) Name() string                         { return "sockets" }
func (socketsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (socketsProvider) Schema() *arrow.Schema                { return socketsTable(nil).Schema() }

// socketRow flattens (host bundle, socket index).
type socketRow struct {
	host        string
	collectedAt int64
	receivedAt  int64
	info        *sysmsnap.SocketInfo
}

func (p socketsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []socketRow
	for _, hs := range p.holder.Hosts() {
		if hs.Snap.Sockets == nil {
			continue
		}
		for i := range hs.Snap.Sockets.Sockets {
			rows = append(rows, socketRow{
				host:        hs.Host,
				collectedAt: hs.Snap.Sockets.CollectedAtUnixMs,
				receivedAt:  hs.ReceivedAtUnixMs,
				info:        &hs.Snap.Sockets.Sockets[i],
			})
		}
	}
	return socketsTable(rows).Build(proj, len(rows)), nil
}

func socketsTable(rows []socketRow) *introspect.Table {
	return introspect.NewTable().
		String("host", func(i int) string { return rows[i].host }).
		String("proto", func(i int) string { return string(rows[i].info.Proto) }).
		String("addr", func(i int) string { return rows[i].info.Addr }).
		Int32("port", func(i int) int32 { return int32(rows[i].info.Port) }).
		Int64("pid", func(i int) int64 { return int64(rows[i].info.PID) }).
		Int64("uid", func(i int) int64 { return int64(rows[i].info.UID) }).
		Uint64("inode", func(i int) uint64 { return rows[i].info.Inode }).
		Int64("collected_at_unix_ms", func(i int) int64 { return rows[i].collectedAt }).
		Int64("received_at_unix_ms", func(i int) int64 { return rows[i].receivedAt })
}
