// Demo-registry enrollment for imzrt (ADR-0057). This replaces the former
// per-app screenshot tour: instead of a settle/capture/advance state machine
// (with its own SIGTERM-on-complete exit) driven by a screenshot-mode
// SeededFuncApp, each dashboard tab (heap / gc / sched) registers as its own
// Demo whose body is one full-width panel rendered into the host Ui scope. The
// central TestDriver (widgets) captures one PNG per tab.
//
// imzrt's values are the Go runtime's own live metrics, so captures are not
// byte-stable across runs — every Demo is flagged NonDeterministic and the
// TestDriver skips them under IMZERO2_SCREENSHOT_DETERMINISTIC. The shared
// sampler is started/tuned at Init (which the TestDriver runs before the
// capture loop), so the rings hold enough history to draw every plot by the
// time a tab is captured.

package imzrt

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourSamplerPeriod tightens the sampler cadence for capture: at 100 ms the
// rings accumulate history fast enough that every line plot and a readable run
// of spectrogram columns are populated before the shot.
const tourSamplerPeriod = 100 * time.Millisecond

// imzrtScenes is one entry per registered Demo. scene selects which panel
// renderTourScene draws full-width.
var imzrtScenes = []struct {
	name  string
	scene string
	title string
	desc  string
}{
	{"imzrt-heap", "heap", icons.PhPulse + " imzrt — heap",
		"imzrt's heap panel — live Go runtime heap metrics rendered as sliding-window line plots."},
	{"imzrt-gc", "gc", icons.PhPulse + " imzrt — GC",
		"imzrt's GC panel — live garbage-collector metrics over a sliding window."},
	{"imzrt-sched", "sched", icons.PhPulse + " imzrt — scheduler",
		"imzrt's scheduler panel — live scheduler-latency metrics, including the latency spectrogram."},
}

func init() {
	for _, sc := range imzrtScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Tools",
			Title:          sc.title,
			Stage:          [2]float32{1200, 800},
			Flags:          registry.DemoFlagNonDeterministic | registry.DemoFlagNeedsLargeArea,
			Kind:           registry.DemoKindUX,
			Description:    sc.desc,
			Init:           makeTourInit(sc.scene),
			RenderStateful: tourRenderStateful,
			SourceFunc:     (*App).renderTourScene,
		})
	}
}

// imzrtDemoState is the per-Demo state: the App instance bound to the host id
// stack plus the scene it renders. The Sampler is a process singleton reached
// via ensureSampler, so it is not held here.
type imzrtDemoState struct {
	app   *App
	scene string
}

// makeTourInit returns an Init that builds an imzrt App bound to the host id
// stack and tunes the shared sampler for capture cadence. ensureSampler starts
// the sampler on first call; tuning once per Demo Init is harmless.
func makeTourInit(scene string) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := newApp()
		inst.ids = ids
		if s, err := ensureSampler(); err == nil && s != nil {
			s.SetInterval(tourSamplerPeriod)
		}
		state = &imzrtDemoState{app: inst, scene: scene}
		return
	}
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	st, ok := state.(*imzrtDemoState)
	if !ok || st == nil {
		return
	}
	st.app.ids = ids
	s, err := ensureSampler()
	if err != nil {
		renderInitErrorPanel(err)
		return
	}
	snap := s.Latest()
	if snap == nil {
		c.Label("imzrt: waiting for first sample…").Send()
		return
	}
	st.app.renderTourScene(snap, s, st.scene)
}

// renderTourScene draws the top bar plus one panel full-width (no dock), so each
// scene captures a single tab cleanly. Mirrors interactive layout otherwise.
func (inst *App) renderTourScene(snap *PublishedSnapshot, s *Sampler, scene string) {
	for range c.PanelTopInside(inst.ids.PrepareStr("imzrt-topbar")).Resizable(false).KeepIter() {
		inst.renderTopBar(snap, s)
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			switch scene {
			case "gc":
				inst.renderGCPanel(snap)
			case "sched":
				inst.renderSchedPanel(snap)
			default:
				inst.renderHeapPanel(snap)
			}
		}
	}
}
