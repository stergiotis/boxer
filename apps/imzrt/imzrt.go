package imzrt

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Sampler is a process-wide singleton: there is exactly one Go runtime per
// process, so one shared history is the correct model (ADR-0061 SD3). Per-window
// UI state lives on the *App the registry hands back from each Open().
var (
	samplerOnce sync.Once
	sampler     *Sampler
	samplerErr  error
)

// App is the per-window imzrt instance. The registry's factory ctor allocates a
// fresh App per Open(); all instances read the one shared Sampler. M1 carries
// only chrome state (the WidgetIdStack and the spacing density); user-visible
// per-window selections land here as later milestones add panels with controls.
type App struct {
	// ids is the per-instance WidgetIdStack the host pre-prepares with a
	// window-unique salt every frame. Captured from MountCtx.Ids() at Mount; the
	// ctor seeds a fresh stack so tour mode and tests work without a Mount call.
	ids *c.WidgetIdStack
	// density resolves IDS spacing tokens at the active preset (ADR-0032 §SD2).
	density styletokens.DensityE
	// schedSpectro is the per-window scheduling-latency spectrogram state
	// (heatmapscroll texture + colormap), built lazily on the first frame that
	// carries the histogram's bucket layout. See imzrt_panel_sched.go.
	schedSpectro schedSpectroState
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:     c.NewWidgetIdStack(),
		density: styletokens.DensityFromEnv(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// Frame renders one frame of the imzrt window body. The host has already
// pre-pushed a window-unique salt onto inst.ids, so widget ids derived from it
// are scoped per window.
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
			log.Error().Err(buildErr).Msg("imzrt: sampler init failed")
			return
		}
		built.Start(context.Background())
		sampler = built
	})
	s = sampler
	err = samplerErr
	return
}

// Stable dock tab identifiers. egui_dock keys its persistent layout state off
// them, so they must not be reused across tabs. M3 adds the Scheduler tab.
const (
	dockTabHeap  uint64 = 1
	dockTabGC    uint64 = 2
	dockTabSched uint64 = 3
)

// renderApp lays the body out inside the runtime-created window scope: a pinned
// top bar plus a DockArea holding the Heap and GC tabs. M3 adds the Scheduler tab
// to the same leaf (ADR-0061).
func (inst *App) renderApp(snap *PublishedSnapshot, s *Sampler) {
	if snap == nil {
		for range c.PanelCentralInside().KeepIter() {
			c.Label("imzrt: waiting for first sample…").Send()
		}
		return
	}

	for range c.PanelTopInside(inst.ids.PrepareStr("imzrt-topbar")).Resizable(false).KeepIter() {
		inst.renderTopBar(snap, s)
	}
	for range c.PanelCentralInside().KeepIter() {
		for dock := range c.DockArea(inst.ids.PrepareStr("imzrt-dock")) {
			dock.InitRoot(dockTabHeap, dockTabGC, dockTabSched)
			for range dock.Tab(dockTabHeap, "Heap") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderHeapPanel(snap)
				}
			}
			for range dock.Tab(dockTabGC, "GC") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderGCPanel(snap)
				}
			}
			for range dock.Tab(dockTabSched, "Scheduler") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderSchedPanel(snap)
				}
			}
		}
	}
}

func renderInitErrorPanel(err error) {
	for range c.PanelCentralInside().KeepIter() {
		c.Label("imzrt: sampler init failed").Send()
		c.Label(err.Error()).Send()
	}
}

// sectionHeader renders a bold panel sub-heading followed by a horizontal rule.
func (inst *App) sectionHeader(title string) {
	for rt := range c.RichTextLabel(title) {
		rt.Strong()
	}
	c.Separator().Send()
}
