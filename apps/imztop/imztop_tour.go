// Demo-registry enrollment for imztop (ADR-0057). This replaces the former
// per-app screenshot tour: instead of a settle/capture/advance state machine
// (with its own SIGTERM-on-complete exit) driven by a screenshot-mode
// SeededFuncApp, the unfiltered and filtered process views register as Demos
// whose body is the imztop dashboard rendered into the host Ui scope. The
// central TestDriver (widgets) captures one PNG per scene.
//
// imztop's values are live system metrics (CPU%, memory, processes, GPU), so
// captures are not byte-stable across runs — every Demo is flagged
// NonDeterministic and the TestDriver skips them under
// IMZERO2_SCREENSHOT_DETERMINISTIC. The shared sampler is started/tuned at Init
// (which the TestDriver runs before the capture loop), so plots have history by
// the time a scene is captured.

package imztop

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmscrape"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourSamplerPeriod tightens the sampler cadence for capture: at 100 ms the
// rings accumulate roughly ten history points per settle window — enough to
// draw every panel's line plot and the per-core sparkline grid.
const tourSamplerPeriod = 100 * time.Millisecond

// imztopScenes is one entry per registered Demo: a name plus the process-table
// filter to pin before rendering.
var imztopScenes = []struct {
	name   string
	filter string
	title  string
	desc   string
}{
	{"imztop-running", "", icons.PhGauge + " imztop — processes",
		"imztop's live system monitor — a docked layout of CPU/memory/network/disk/GPU/sensors panels plus the process table, unfiltered."},
	{"imztop-filtered", "imzero2", icons.PhGauge + " imztop — filtered",
		"The same monitor with the process table filtered to \"imzero2\"."},
}

func init() {
	for _, sc := range imztopScenes {
		registry.Register(registry.Demo{
			Name:           sc.name,
			Category:       "Tools",
			Title:          sc.title,
			Stage:          [2]float32{1200, 800},
			Flags:          registry.DemoFlagNonDeterministic | registry.DemoFlagNeedsLargeArea,
			Kind:           registry.DemoKindUX,
			Description:    sc.desc,
			Init:           makeTourInit(sc.filter),
			RenderStateful: tourRenderStateful,
			SourceFunc:     (*App).renderApp,
		})
	}
}

// imztopDemoState is the per-Demo state: the App instance bound to the host id
// stack plus the process-table filter this scene pins. The Sampler is a process
// singleton reached via ensureSampler, so it is not held here.
type imztopDemoState struct {
	app    *App
	filter string
}

var tourScraperOnce sync.Once

// ensureTourScraper wires a co-located in-proc scraper for the screenshot tour,
// which runs without a host bus: an inprocbus carries StartScraper's published
// bundles to the singleton consumer Sampler. Idempotent; the scraper runs for
// the process lifetime (the tour is a capture harness). This is the one place
// the imztop package still reaches the collectors — only on the tour path, not
// in the production App; full capslock-clean SD6 would relocate it (tracked).
func ensureTourScraper() {
	tourScraperOnce.Do(func() {
		bus := inprocbus.NewInst(log.Logger)
		pub := bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
		})
		sub := bus.NewClient(manifest.Id, []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub},
		})
		setSamplerBus(sub)
		if _, err := sysmscrape.StartScraper(context.Background(), pub, sysmetricsbus.DefaultHostToken(), tourSamplerPeriod, log.Logger); err != nil {
			log.Warn().Err(err).Msg("imztop tour: scraper unavailable; panels will be empty")
		}
	})
}

// makeTourInit returns an Init that builds an imztop App bound to the host id
// stack, wires the tour-local scraper, and tunes the EWMA cadence for capture.
func makeTourInit(filter string) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := newApp()
		inst.ids = ids
		ensureTourScraper()    // the tour has no host bus; feed the consumer locally
		_, _ = ensureSampler() // start the singleton consumer; the scraper sets the cadence
		state = &imztopDemoState{app: inst, filter: filter}
		return
	}
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	st, ok := state.(*imztopDemoState)
	if !ok || st == nil {
		return
	}
	// The process-table filter is a package global; set it per-frame for
	// this Demo (Init runs for every Demo at setup, so the last writer would
	// win there). renderApp draws a "waiting for first sample" placeholder
	// when the snapshot is still nil.
	setProcFilter(st.filter)
	st.app.ids = ids
	s, err := ensureSampler()
	if err != nil {
		renderInitErrorPanel(err)
		return
	}
	st.app.renderApp(s.Latest(), s)
}
