// Demo-registry enrollment for imztop (ADR-0057). This replaces the former
// per-app screenshot tour: instead of a settle/capture/advance state machine
// (with its own SIGTERM-on-complete exit) driven by a screenshot-mode
// SeededFuncApp, the unfiltered and filtered process views register as Demos
// whose body is the imztop dashboard rendered into the host Ui scope. The
// central TestDriver (widgets) captures one PNG per scene.
//
// The panels are fed a synthetic, live-looking metric stream (tourSampler) over
// an in-proc bus rather than a real /proc scrape, so package imztop imports no
// collector even for capture (ADR-0090 SD6 — fully closed). The values still
// wander per tick and are time-seeded, so captures are not byte-stable — every
// Demo is flagged NonDeterministic and the TestDriver skips them under
// IMZERO2_SCREENSHOT_DETERMINISTIC. The shared sampler + feed start at Init
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
	name     string
	filter   string
	title    string
	desc     string
	activate uint64 // dock tab to force active for the capture; 0 = default (CPU)
}{
	{"imztop-running", "", icons.PhGauge + " imztop — processes",
		"imztop's live system monitor — a docked layout of CPU/memory/network/disk/GPU/sensors panels plus the process table, unfiltered.", 0},
	{"imztop-filtered", "imzero2", icons.PhGauge + " imztop — filtered",
		"The same monitor with the process table filtered to \"imzero2\".", 0},
	// Proc Map scene is LAST: it forces its tab active every frame, which leaves
	// the shared dock state on Proc Map, so any scene after it would inherit that.
	{"imztop-procmap", "", icons.PhGauge + " imztop — process map",
		"The process tree as a treemap: processes nested parent → child, each box sized by resident memory and tinted by CPU load.", dockTabProcMap},
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
			Init:           makeTourInit(sc.filter, sc.activate),
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

var tourFeedOnce sync.Once

// ensureTourFeed wires a co-located in-proc SYNTHETIC metric feed for the
// screenshot tour, which runs without a host bus: an inprocbus carries a
// sysmetricsbus.Producer's published bundles to the singleton consumer Sampler,
// but the Producer samples a synthetic source ([tourSampler]) rather than the
// /proc collectors. Idempotent; runs for the process lifetime (the tour is a
// capture harness). Feeding synthetic data is what lets package imztop import
// zero collectors even here — the last §SD6 in-package reach is closed.
func ensureTourFeed() {
	tourFeedOnce.Do(func() {
		bus := inprocbus.NewInst(log.Logger)
		pub := bus.NewClient(sysmetricsbus.ServiceAppId, []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionPub},
		})
		sub := bus.NewClient(manifest.Id, []app.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: app.CapDirectionSub},
		})
		setSamplerBus(sub)
		producer, err := sysmetricsbus.NewProducer(sysmetricsbus.ProducerOptions{
			Bundle:   newTourSampler(),
			Bus:      pub,
			Subject:  sysmetricsbus.BundleSubject(sysmetricsbus.DefaultHostToken()),
			Codec:    sysmetricsbus.NewCBORCodec(),
			Interval: tourSamplerPeriod,
			Log:      log.Logger,
		})
		if err != nil {
			log.Warn().Err(err).Msg("imztop tour: synthetic feed unavailable; panels will be empty")
			return
		}
		producer.Start(context.Background())
	})
}

// makeTourInit returns an Init that builds an imztop App bound to the host id
// stack, wires the tour-local synthetic feed, and starts the consumer.
func makeTourInit(filter string, activate uint64) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := newApp()
		inst.ids = ids
		inst.activateTab = activate // 0 for most scenes; the Proc Map scene targets its tab
		ensureTourFeed()            // the tour has no host bus; feed the consumer locally
		_, _ = ensureSampler()      // start the singleton consumer; the feed sets the cadence
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
