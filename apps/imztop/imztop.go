//go:build llm_generated_opus47

package imztop

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Sampler is a system-wide singleton: one OS sampler feeds every open
// imztop window. Per-window state (current network interface, future
// per-window selections) lives on the *App value the registry hands
// back from each Open().
var (
	samplerOnce sync.Once
	sampler     *Sampler
	samplerErr  error
)

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
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }
func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
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
		built, buildErr := NewSampler(SamplerOptions{})
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
	dockTabCPU     uint64 = 1
	dockTabMem     uint64 = 2
	dockTabBattery uint64 = 3
	dockTabSensors uint64 = 4
	dockTabDisk    uint64 = 5
	dockTabNet     uint64 = 6
	dockTabGPU     uint64 = 7
	dockTabProc    uint64 = 8
)

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
			cpuLeaf := dock.InitRoot(dockTabCPU, dockTabMem, dockTabBattery, dockTabSensors)
			_ = dock.Split(cpuLeaf, c.DockBelow, 0.55, dockTabProc) // PROC at bottom (~45%)
			_ = dock.Split(cpuLeaf, c.DockRight, 0.27, dockTabNet, dockTabDisk, dockTabGPU)

			for range dock.Tab(dockTabCPU, "CPU") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderCPUPanel(snap)
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
