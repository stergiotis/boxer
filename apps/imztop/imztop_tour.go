//go:build llm_generated_opus47

package imztop

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// tourExitGraceDelay is how long the tour waits between deciding "done"
// and sending SIGTERM to itself. The last RequestScreenshot is async —
// Rust writes the PNG some frames after the Go-side request — so an
// immediate exit would truncate the in-flight capture. Empirically the
// gap is ~1s; we round to 2s for headroom.
const tourExitGraceDelay = 2 * time.Second

// scheduleTourExit kicks off a one-shot goroutine that SIGTERMs the
// process after tourExitGraceDelay. SIGTERM is handled by the runtime
// (cli's reapAll + boxer's flightRecorder.FlushOnSignal handler, which
// calls os.Exit after the flush). Multiple invocations are coalesced
// via sync.Once so the early-skip path, the success path, and the
// init-error path all share one timer.
var tourExitOnce sync.Once

func scheduleTourExit(reason string) {
	tourExitOnce.Do(func() {
		log.Info().Str("reason", reason).Dur("after", tourExitGraceDelay).Msg("imztop tour: SIGTERM scheduled")
		go func() {
			time.Sleep(tourExitGraceDelay)
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
	})
}

// Tour timing tuned so plots have history points at capture time. The
// default 1-Hz sampler combined with a 6-frame (≈100 ms) settle window
// left rings with a single sample, gating every PlotLine guarded by
// `len(HistoryTimeUnixSec) >= 2` and producing flat, plot-less captures.
//
// At a 100 ms sampler interval and a 60-frame settle window (≈1 s) we
// land roughly 10 history points before the screenshot fires — enough
// to draw every panel's line plot and the per-core sparkline grid.
const (
	tourSettleFrames  int32         = 60
	tourSamplerPeriod time.Duration = 100 * time.Millisecond
)

type tourPhaseE uint8

const (
	tourPhaseSettle tourPhaseE = iota
	tourPhaseCapture
	tourPhaseAdvance
)

type tourScene struct {
	Name  string
	Setup func()
}

var tourScenes = []tourScene{
	{
		Name: "running",
		Setup: func() {
			setProcFilter("")
		},
	},
	{
		Name: "filtered",
		Setup: func() {
			setProcFilter("imzero2")
		},
	},
}

type tourStateS struct {
	setupDone       bool
	samplerTuned    bool
	doneAll         bool
	sceneIdx        int32
	sceneAppliedFor int32
	phase           tourPhaseE
	settleCnt       int32
	waitingForSnap  bool

	// tourApp is the App instance the tour renders through. Allocated
	// on first use so the renderApp method has a receiver; per-window
	// fields stay at their defaults (index 0, etc.) because tour mode
	// is single-instance and reproducibility trumps interactivity.
	tourApp *App
}

var tourState = tourStateS{sceneAppliedFor: -1}

// RenderLoopHandlerTour is the screenshot-tour entry point. Activates
// when IMZERO2_SCREENSHOT_DIR is set; the dispatcher selects this
// handler over RenderLoopHandlerDemo. The seed comes from
// SeededFuncApp (see [RenderLoopHandlerDemo] for the multi-instance
// rationale; in tour mode it's typically single-instance but the
// IdScope guards against drift).
func RenderLoopHandlerTour(seed uint64) (err error) {
	screenshotDir := imzero2env.ScreenshotDir.Get()
	if screenshotDir == "" {
		return
	}
	// Honor IMZERO2_SCREENSHOT_DETERMINISTIC — imztop's value is live
	// sysmetrics (CPU%, memory, processes, GPU); there's no way to make
	// the captures byte-stable without faking the data entirely. Skip
	// the whole tour when the deterministic gate is set; default mode
	// (empty env var) still captures the two scenes as before.
	if imzero2env.ScreenshotDeterministic.Get() != "" {
		if !tourState.doneAll {
			log.Info().Str("dir", screenshotDir).Msg("imztop tour: skipped (IMZERO2_SCREENSHOT_DETERMINISTIC set)")
			tourState.doneAll = true
			scheduleTourExit("deterministic skip")
		}
		return
	}
	if !tourState.setupDone {
		mkErr := os.MkdirAll(screenshotDir, 0o755)
		if mkErr != nil {
			log.Warn().Err(mkErr).Str("dir", screenshotDir).Msg("imztop tour: unable to create output dir")
		}
		c.SetAnimationFreeze(true)
		c.MemoryResetAreas()
		log.Info().Int("scenes", len(tourScenes)).Str("dir", screenshotDir).Msg("imztop tour: starting")
		tourState.setupDone = true
	}
	c.RequestRepaint()

	if tourState.doneAll {
		return
	}
	if int(tourState.sceneIdx) >= len(tourScenes) {
		log.Info().Str("dir", screenshotDir).Msg("imztop tour: complete")
		tourState.doneAll = true
		scheduleTourExit("tour complete")
		return
	}

	scene := &tourScenes[tourState.sceneIdx]
	if tourState.sceneAppliedFor != tourState.sceneIdx {
		scene.Setup()
		tourState.sceneAppliedFor = tourState.sceneIdx
	}

	s, sErr := ensureSampler()
	if sErr != nil {
		renderInitErrorPanel(sErr)
		if tourState.phase == tourPhaseCapture {
			path := filepath.Join(screenshotDir, "imztop_init_error.png")
			if w, h, ok := imzero2env.ScreenshotSizeWH(); ok {
				c.RequestScreenshotRect(path, 0, 0, float32(w), float32(h))
			} else {
				c.RequestScreenshot(path)
			}
			tourState.doneAll = true
			scheduleTourExit("sampler init error")
		} else {
			tourState.phase = tourPhaseCapture
		}
		return
	}

	if !tourState.samplerTuned && s != nil {
		s.SetInterval(tourSamplerPeriod)
		tourState.samplerTuned = true
		log.Info().Dur("interval", tourSamplerPeriod).Msg("imztop tour: sampler tuned for capture cadence")
	}

	snap := s.Latest()
	if snap == nil {
		// First-tick latency: hold the tour state still until the sampler
		// publishes. Don't burn frames against an empty render.
		tourState.waitingForSnap = true
		c.Label("Imztop tour: waiting for first sample…").Send()
		return
	}
	tourState.waitingForSnap = false

	// Tour reuses the App struct's renderer so the layout stays
	// identical to interactive mode. The transient App is allocated
	// once (tourState.tourApp) and reused across frames so any future
	// per-window state additions get a stable container even in
	// screenshot mode. The tour's own IdScope wrapper seeds tourApp.ids
	// with the tour's seed value so the captured ids stay reproducible
	// across runs.
	if tourState.tourApp == nil {
		tourState.tourApp = newApp()
	}
	tourState.tourApp.ids.Reset()
	for range c.IdScope(tourState.tourApp.ids.PrepareSeq(seed)) {
		tourState.tourApp.renderApp(snap, s)
	}

	switch tourState.phase {
	case tourPhaseSettle:
		tourState.settleCnt++
		if tourState.settleCnt >= tourSettleFrames {
			tourState.phase = tourPhaseCapture
		}
	case tourPhaseCapture:
		path := filepath.Join(screenshotDir, "imztop_"+scene.Name+".png")
		// IMZERO2_SCREENSHOT_SIZE narrows the capture to a fixed WxH
		// rect (matching the widgets TestDriver). hmi.sh resizes the
		// eframe viewport to the same WxH so imztop's central-panel
		// layout fills the rect exactly. Empty / malformed env →
		// keep the legacy full-viewport capture. ADR-0057 SD5.
		if w, h, ok := imzero2env.ScreenshotSizeWH(); ok {
			c.RequestScreenshotRect(path, 0, 0, float32(w), float32(h))
		} else {
			c.RequestScreenshot(path)
		}
		log.Info().Str("path", path).Str("scene", scene.Name).Msg("imztop tour: capture requested")
		tourState.phase = tourPhaseAdvance
	case tourPhaseAdvance:
		tourState.sceneIdx++
		tourState.phase = tourPhaseSettle
		tourState.settleCnt = 0
		c.MemoryResetAreas()
	}
	return
}
