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
	activate uint64 // dock tab this scene forces active for its capture (every scene sets one)
	replay   bool   // scene captures replay mode rather than live (ADR-0197 M5)
}{
	// Each scene forces its own tab active (activateTab), never relying on capture
	// order: the TestDriver renders all scenes through one shared id-stack → one
	// shared dock state, and captures them sorted by Name, so without an explicit
	// activate a scene would inherit whatever tab a prior capture left active.
	{"imztop-running", "", icons.PhGauge + " imztop — processes",
		"imztop's live system monitor — a docked layout of CPU/memory/network/disk/GPU/sensors panels plus the process table, unfiltered.", dockTabCPU, false},
	{"imztop-filtered", "imzero2", icons.PhGauge + " imztop — filtered",
		"The same monitor with the process table filtered to \"imzero2\".", dockTabCPU, false},
	{"imztop-procmap", "", icons.PhGauge + " imztop — process map",
		"The process tree as a treemap: processes nested parent → child, each box sized by resident memory and tinted by CPU load.", dockTabProcMap, false},
	{"imztop-replay", "", icons.PhClockCounterClockwise + " imztop — replay",
		"The same monitor replaying stored history instead of live data (ADR-0197): a transport above, an availability strip showing where the tee recorded and how busy the box was, and the panels drawing frames that carry the times they were recorded at.", dockTabCPU, true},
	{"imztop-replay-notrecorded", "", icons.PhClockCounterClockwise + " imztop — replay, not recorded",
		"What replay cannot show (ADR-0197 §SD8). The persistence tee stores no sensors kind, so the Sensors tab is empty in replay for a reason that is about the recording rather than about the machine — and says so, rather than reading as a host with no temperatures.", dockTabSensors, true},
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
			Init:           makeTourInit(sc.filter, sc.activate, sc.replay),
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
	// replay makes this scene capture the replay surface. Asserted per frame
	// rather than at Init because the session is process-wide and the driver
	// captures scenes in name order, so a scene must not inherit the mode a
	// previous one left behind.
	replay bool
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
func makeTourInit(filter string, activate uint64, replay bool) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := newApp()
		inst.ids = ids
		// Showcase the smoothing overlay in captures (ADR-0152 wiring); the
		// interactive default stays off.
		inst.smooth.On = true
		inst.activateTab = activate // 0 for most scenes; the Proc Map scene targets its tab
		ensureTourFeed()            // the tour has no host bus; feed the consumer locally
		_, _ = ensureSampler()      // start the singleton consumer; the feed sets the cadence
		state = &imztopDemoState{app: inst, filter: filter, replay: replay}
		return
	}
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	st, ok := state.(*imztopDemoState)
	if !ok || st == nil {
		return
	}
	// Set this Demo's filter per-frame on its own App: Init runs for every
	// Demo at setup, so a shared setter would leave the last writer's filter
	// in place. renderApp draws a "waiting for first sample" placeholder when
	// the snapshot is still nil.
	st.app.setProcFilter(st.filter)
	st.app.ids = ids
	if st.replay {
		// Replay scene: install the synthetic session (idempotent) and draw it.
		if s := ensureTourReplay(); s != nil {
			st.app.renderApp(s.Latest(), s)
			return
		}
	}
	// Live scene — or a replay scene whose session could not be built. Either
	// way, assert live rather than inherit whatever ran before.
	ensureTourLive()
	s, err := ensureSampler()
	if err != nil {
		renderInitErrorPanel(err)
		return
	}
	st.app.renderApp(s.Latest(), s)
}
