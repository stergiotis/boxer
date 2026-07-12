package imztop

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/lazypane"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// Sampler is a system-wide singleton: one OS sampler feeds every open
// imztop window. Per-window state (current network interface, future
// per-window selections) lives on the *App value the registry hands
// back from each Open().
var (
	samplerOnce sync.Once
	sampler     *Sampler
	samplerErr  error

	samplerBusMu sync.Mutex
	samplerBus   app.BusI
)

// setSamplerBus records the bus the singleton Sampler subscribes on. The first
// non-nil bus wins — captured from the first window's MountCtx.Bus() (carousel),
// or set by the tour/tests before ensureSampler. Ignored once the Sampler is
// built (samplerOnce has fired).
func setSamplerBus(bus app.BusI) {
	if bus == nil {
		return
	}
	samplerBusMu.Lock()
	if samplerBus == nil {
		samplerBus = bus
	}
	samplerBusMu.Unlock()
}

// App is the per-window imztop instance. The registry's factory ctor
// allocates a fresh App per Open() so two windows have independent UI
// state (currently just the selected network interface; more fields
// land here as user-visible per-window state grows).
type App struct {
	// ids is the per-instance WidgetIdStack the host pre-prepares
	// with a window-unique salt every frame (windowhost wraps Frame
	// in c.IdScope keyed on the window key). Captured from
	// MountCtx.Ids() at Mount time. The ctor seeds it with a fresh
	// stack so tour mode and tests work without a Mount call.
	ids *c.WidgetIdStack

	// lazyPanes holds one widgets/lazypane gate per heavy dock tab, keyed
	// by dock id and created on first use. While a tab is hidden the host
	// discards its body buffer, so the pane emits only a probe + loading
	// placeholder and skips the panel render; the CPU spectrograms
	// (heatmapscroll / scrollingTexture) and the Proc Map treemap are the
	// bodies that make this worth it. Persistent render-thread state — each
	// pane carries its hidden/warming/live phase machine across frames.
	lazyPanes map[uint64]*lazypane.Pane

	// tasks is the keelson task API (task.ForApp at Mount). The embedded
	// distsummary widgets thread it into their ECDF band warm-up so the
	// O(n²) inversion runs as a background job (ADR-0038) instead of on
	// the render thread. Zero value (tour/tests, no Mount) is nil — the
	// band still warms off-thread, just without task-framework audit.
	tasks task.TaskApiI

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE

	// netSelectedIfaceIdx is the index of the network interface the
	// user picked from this window's ComboBox. Defaults to 0; auto-
	// clamps if the sampler drops interfaces between frames.
	netSelectedIfaceIdx int

	// procFilterDraft is this window's in-flight TextEdit value for
	// the process-panel filter. Must be a persistent field — the
	// SendRespVal binding writes the user's keystrokes here between
	// frames, and the next frame's render reads it back as the
	// TextEdit's displayed value. A local var would be reset to
	// view.Filter each frame and the typed text would never appear.
	// Pushed into the package-global procView (the sampler's source
	// of truth) on every HasChanged response.
	procFilterDraft string

	// cpuHeatmap holds the per-core CPU% scrolling heatmap state
	// (HeatmapScroll widget + colormap config + last-published
	// timestamp). Lazy-initialised on the first frame that carries a
	// non-empty PerCorePercent slice — the height (number of rows)
	// locks to the core count seen there. See imztop_cpu_heatmap.go.
	cpuHeatmap cpuHeatmapState

	// cpuCoresDigest summarises the cross-core CPU% distribution at the
	// current instant (one sample per logical CPU). cpuHistoryDigest
	// summarises the temporal distribution of aggregate CPU% over the
	// sampler's history window. Both are rebuilt each frame via
	// Reset+Push — keeping them on *App avoids a per-frame heap
	// allocation under ImZero2's continuous-repaint loop. See
	// renderCPUDistsummaries in imztop_panel_cpu.go.
	cpuCoresDigest   *tdigest.TDigest
	cpuHistoryDigest *tdigest.TDigest

	// diskDistsumDigest / gpuDistsumDigest summarise the cross-device
	// utilization distribution (block-device busy%, GPU busy%). Both
	// metrics are intrinsically % so the summary is commensurable.
	// Net intentionally omitted: utilization there requires a link-
	// capacity reading the net collector doesn't yet expose, and a
	// throughput-based summary would mix heterogeneous interfaces.
	// Same Reset+Push-per-frame idiom as the CPU pair; see
	// renderPerDeviceDistsummary in imztop_panel_cpu.go.
	diskDistsumDigest *tdigest.TDigest
	gpuDistsumDigest  *tdigest.TDigest

	// Topology panel state (imztop_panel_topology.go). The CPU topology is
	// static and arrives on the metric plane (ADR-0090 SD6); the treemap is
	// built once, on the first topology-bearing snapshot (initTopology), which
	// is also when inst.ids is the post-Mount stack the treemap must bind to.
	//
	//   topoTreemap  the squarify widget; nil until built / on error.
	//   topoNodeObj    layout node → source TopoObject (live tint + hover detail).
	//   topoLoad       per-frame per-core busy%; aliases the snapshot slice.
	//   topoFreq       per-frame per-core MHz; aliases the snapshot slice.
	//   topoFreqMaxMHz running max core MHz, for normalising the freq tint.
	//   topoDim        which dimension the continuous tint encodes (% or MHz).
	topoTreemap    *treemap.Treemap
	topoNodeObj    map[*layout.Node]*sysmsnap.TopoObject
	topoLoad       []uint8
	topoFreq       []uint32
	topoFreqMaxMHz uint32
	topoDim        topoDimE

	// Colorscale legend state (imztop_panel_topology.go). topoScaleMax is the
	// value the gradient tops out at in real units (100 for %, the smoothed
	// peak MHz for frequency); it is the shared denominator for the tint and
	// the legend so the two agree. topoScale is rebuilt (keyed by topoScaleKey)
	// only when the dimension or rounded max changes. topoLastSampleMs gates
	// the frequency-max smoothing to once per new sample.
	topoScaleMax     uint32
	topoScale        *colorscale.ColorScale
	topoScaleKey     string
	topoLastSampleMs int64

	// topoActive aliases the latest cgroup-effective cpuset (CPUSnapshot.ActiveCPUs);
	// PU boxes whose logical CPU is outside it render inactive (greyed).
	topoActive []int32

	// Proc Map panel state (imztop_panel_procmap.go). The process treemap nests
	// live processes by PPID. Unlike the static topology tree it is rebuilt from
	// every sample (reconcileProcTree), so process nodes are pooled by
	// (PID,StartedAt) to keep the treemap's drill state stable across rebuilds.
	//
	//   procTreemap      the squarify widget; nil until the panel is first shown.
	//   procRoot         stable synthetic forest root the widget is anchored to.
	//   procNodes        pooled process node by EWMA key (stable pointer identity).
	//   procNodeObj      layout node → source process + smoothed CPU% (tint + hover).
	//   procMetric       the area encoding the user picked (RSS default / CPU%).
	//   procBuiltMetric / procLastSampleMs gate the rebuild to sample/metric changes.
	procTreemap      *treemap.Treemap
	procRoot         *layout.Node
	procNodes        map[procEWMAKey]*layout.Node
	procNodeObj      map[*layout.Node]*procCell
	procMetric       procMetricE
	procBuiltMetric  procMetricE
	procLastSampleMs int64
	// procCores is the logical-core count, for the CPU-load tint normalisation
	// (per-process CPU% ranges [0, cores*100]). Refreshed from the snapshot.
	procCores int

	// activateTab, when non-zero, forces a dock tab active each frame. Zero
	// (the production default) leaves tab focus entirely to the user; only the
	// screenshot tour sets it, so a capture can target an otherwise-hidden tab.
	activateTab uint64
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:               c.NewWidgetIdStack(),
		density:           styletokens.DensityFromEnv(),
		cpuCoresDigest:    tdigest.NewTDigest(),
		cpuHistoryDigest:  tdigest.NewTDigest(),
		diskDistsumDigest: tdigest.NewTDigest(),
		gpuDistsumDigest:  tdigest.NewTDigest(),
		lazyPanes:         map[uint64]*lazypane.Pane{},
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.tasks = task.ForApp(ctx)
	// Capture the host-provided capability bus for the singleton Sampler to
	// subscribe on (ADR-0090): imztop consumes metrics, it never reads /proc.
	setSamplerBus(ctx.Bus())
	return
}
func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// Frame renders one frame of the imztop window body. The host has
// already pre-pushed a window-unique salt onto inst.ids via c.IdScope
// (windowhost.renderWindowBody), so widget ids derived from inst.ids
// are scoped under that salt — no further package-level coordination
// is needed (every render helper is a method on *App).
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	s, sErr := ensureSampler()
	if sErr != nil {
		renderInitErrorPanel(sErr)
		return
	}
	snap := s.Latest()
	inst.renderApp(snap, s)
	return
}

func ensureSampler() (s *Sampler, err error) {
	samplerOnce.Do(func() {
		samplerBusMu.Lock()
		bus := samplerBus
		samplerBusMu.Unlock()
		built, buildErr := NewSampler(SamplerOptions{}, bus)
		if buildErr != nil {
			samplerErr = buildErr
			log.Error().Err(buildErr).Msg("Imztop: sampler init failed")
			return
		}
		built.Start(context.Background())
		sampler = built
	})
	s = sampler
	err = samplerErr
	return
}

// Stable tab identifiers for the dock area. These must not be reused
// across panels — egui_dock's persistent layout state is keyed off
// them, and the Rust-side reconciler (retain_tabs + push_to_first_leaf)
// trusts them as a stable identity.
const (
	dockTabCPU      uint64 = 1
	dockTabMem      uint64 = 2
	dockTabBattery  uint64 = 3
	dockTabSensors  uint64 = 4
	dockTabDisk     uint64 = 5
	dockTabNet      uint64 = 6
	dockTabGPU      uint64 = 7
	dockTabProc     uint64 = 8
	dockTabTopo     uint64 = 9
	dockTabPressure uint64 = 10
	dockTabProcMap  uint64 = 11
)

// lazyBody gates a heavy dock-tab body through its persistent lazypane,
// created on first use (widgets/lazypane). Call it as the first thing
// inside the tab's for-range and return early on true: while the tab is
// hidden the host discards its body buffer, so the pane emits only a probe
// plus a loading placeholder and the panel render is skipped. title names
// the region in the placeholder and seeds the (stable, unique) probe key.
func (inst *App) lazyBody(dockID uint64, title string) (skip bool) {
	pane := inst.lazyPanes[dockID]
	if pane == nil {
		pane = lazypane.New("imztop-dock-tab-"+title, title)
		inst.lazyPanes[dockID] = pane
	}
	return pane.Skip()
}

// renderApp arranges the body inside the runtime-created window scope
// (ADR-0026 Amendment 2026-05-12: the host wraps Frame in c.Window
// using Manifest.WindowTitle/Icon).
//
// Layout — top bar pinned via PanelTopInside; everything else lives in
// a single DockArea pre-split (via the new InitRoot/Split layout
// descriptor) to mirror the historical static geometry: left column
// stacks CPU/MEM/BATTERY/SENSORS, right column stacks DISK/NET/GPU,
// PROC spans the bottom. Once the user drags a pane, the persistent
// dock_state on the Rust side wins and the initial layout is no
// longer consulted (ADR-0020 follow-on: DockArea pre-split bindings).
func (inst *App) renderApp(snap *PublishedSnapshot, s *Sampler) {
	if snap == nil {
		for range c.PanelCentralInside().KeepIter() {
			c.Label("Imztop: waiting for first sample…").Send()
		}
		return
	}

	for range c.PanelTopInside(inst.ids.PrepareStr("imztop-topbar")).Resizable(false).KeepIter() {
		inst.renderTopBar(snap, s)
	}
	for range c.PanelCentralInside().KeepIter() {
		for dock := range c.DockArea(inst.ids.PrepareStr("imztop-dock")) {
			// Left column groups CPU + slower-changing stats as a single
			// 4-tab leaf with CPU active (first). Right column groups Net
			// + I/O panels as a 3-tab leaf with Net active. PROC spans
			// the bottom on its own leaf. Fewer leaves = more room per
			// panel at the 1280×694 compositor-clamped viewport size.
			cpuLeaf := dock.InitRoot(dockTabCPU, dockTabTopo, dockTabProcMap, dockTabPressure, dockTabMem, dockTabBattery, dockTabSensors)
			_ = dock.Split(cpuLeaf, c.DockBelow, 0.55, dockTabProc) // PROC at bottom (~45%)
			_ = dock.Split(cpuLeaf, c.DockRight, 0.27, dockTabNet, dockTabDisk, dockTabGPU)

			// Tour-only: force a specific tab active so a capture can target an
			// otherwise-hidden one. Zero in production — tab focus stays the
			// user's (ActivateTab does not pin, but calling it every frame in a
			// non-interactive capture keeps the target tab selected).
			if inst.activateTab != 0 {
				dock.ActivateTab(inst.activateTab)
			}

			for range dock.Tab(dockTabCPU, "CPU") {
				// Heaviest panel: imzrt spectrograms drive heatmapscroll
				// (scrollingTexture). Safe to skip while hidden — the ring
				// resets honestly on the starved-texture report when the tab
				// re-shows (the r22 fix; see the lazypane / Lost Sends notes).
				if inst.lazyBody(dockTabCPU, "CPU") {
					continue
				}
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderCPUPanel(snap)
				}
			}
			for range dock.Tab(dockTabTopo, "Topology") {
				if inst.lazyBody(dockTabTopo, "Topology") {
					continue
				}
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderTopologyPanel(snap)
				}
			}
			for range dock.Tab(dockTabProcMap, "Proc Map") {
				if inst.lazyBody(dockTabProcMap, "Proc Map") {
					continue
				}
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderProcMapPanel(snap)
				}
			}
			for range dock.Tab(dockTabPressure, "Pressure") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderPressurePanel(snap)
				}
			}
			for range dock.Tab(dockTabMem, "Memory") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderMemPanel(snap)
				}
			}
			for range dock.Tab(dockTabBattery, "Battery") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderBatteryPanel(snap)
				}
			}
			for range dock.Tab(dockTabSensors, "Sensors") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderSensorsPanel(snap)
				}
			}
			for range dock.Tab(dockTabDisk, "Disk") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderDiskPanel(snap)
				}
			}
			for range dock.Tab(dockTabNet, "Network") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderNetPanel(snap)
				}
			}
			for range dock.Tab(dockTabGPU, "GPU") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderGPUPanel(snap)
				}
			}
			for range dock.Tab(dockTabProc, "Processes") {
				inst.renderProcPanel(snap)
			}
		}
	}
}

func renderInitErrorPanel(err error) {
	for range c.PanelCentralInside().KeepIter() {
		c.Label("Imztop: sampler init failed").Send()
		c.Label(err.Error()).Send()
	}
}
