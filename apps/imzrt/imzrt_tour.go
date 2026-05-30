package imzrt

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

// tourExitGraceDelay is the wait between deciding "done" and SIGTERMing the
// process. The last RequestScreenshot is async (Rust writes the PNG a few frames
// later), so an immediate exit would truncate the in-flight capture.
const tourExitGraceDelay = 2 * time.Second

var tourExitOnce sync.Once

func scheduleTourExit(reason string) {
	tourExitOnce.Do(func() {
		log.Info().Str("reason", reason).Dur("after", tourExitGraceDelay).Msg("imzrt tour: SIGTERM scheduled")
		go func() {
			time.Sleep(tourExitGraceDelay)
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		}()
	})
}

// At a 100 ms sampler interval and a ~1.5 s settle window the rings hold ~15
// points before each capture — enough to draw every line plot and scroll a
// readable run of spectrogram columns.
const (
	tourSettleFrames  int32         = 90
	tourSamplerPeriod time.Duration = 100 * time.Millisecond
)

type tourPhaseE uint8

const (
	tourPhaseSettle tourPhaseE = iota
	tourPhaseCapture
	tourPhaseAdvance
)

// tourScenes capture each dashboard tab as its own full-panel PNG, so the
// spectrogram and every plot are visible without programmatic dock-tab switching.
var tourScenes = []string{"heap", "gc", "sched"}

type tourStateS struct {
	setupDone    bool
	samplerTuned bool
	doneAll      bool
	sceneIdx     int32
	phase        tourPhaseE
	settleCnt    int32

	// tourApp is the App the tour renders through, allocated once and reused so
	// per-window state (the spectrogram texture) has a stable container.
	tourApp *App
}

var tourState tourStateS

// RenderLoopHandlerTour is the screenshot-tour entry point, registered via
// NewSeededFuncApp when IMZERO2_SCREENSHOT_DIR is set. Like imztop, imzrt's
// captures are live values (the Go runtime's own metrics), so they are not
// byte-stable across runs; the IMZERO2_SCREENSHOT_DETERMINISTIC gate skips the
// tour entirely rather than emit churning diffs.
func RenderLoopHandlerTour(seed uint64) (err error) {
	screenshotDir := imzero2env.ScreenshotDir.Get()
	if screenshotDir == "" {
		return
	}
	if imzero2env.ScreenshotDeterministic.Get() != "" {
		if !tourState.doneAll {
			log.Info().Str("dir", screenshotDir).Msg("imzrt tour: skipped (IMZERO2_SCREENSHOT_DETERMINISTIC set)")
			tourState.doneAll = true
			scheduleTourExit("deterministic skip")
		}
		return
	}
	if !tourState.setupDone {
		if mkErr := os.MkdirAll(screenshotDir, 0o755); mkErr != nil {
			log.Warn().Err(mkErr).Str("dir", screenshotDir).Msg("imzrt tour: unable to create output dir")
		}
		c.SetAnimationFreeze(true)
		c.MemoryResetAreas()
		log.Info().Int("scenes", len(tourScenes)).Str("dir", screenshotDir).Msg("imzrt tour: starting")
		tourState.setupDone = true
	}
	c.RequestRepaint()

	if tourState.doneAll {
		return
	}
	if int(tourState.sceneIdx) >= len(tourScenes) {
		log.Info().Str("dir", screenshotDir).Msg("imzrt tour: complete")
		tourState.doneAll = true
		scheduleTourExit("tour complete")
		return
	}

	s, sErr := ensureSampler()
	if sErr != nil {
		renderInitErrorPanel(sErr)
		if tourState.phase == tourPhaseCapture {
			captureScene(screenshotDir, "init_error")
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
	}

	snap := s.Latest()
	if snap == nil {
		c.Label("imzrt tour: waiting for first sample…").Send()
		return
	}

	scene := tourScenes[tourState.sceneIdx]
	if tourState.tourApp == nil {
		tourState.tourApp = newApp()
	}
	tourState.tourApp.ids.Reset()
	for range c.IdScope(tourState.tourApp.ids.PrepareSeq(seed)) {
		tourState.tourApp.renderTourScene(snap, s, scene)
	}

	switch tourState.phase {
	case tourPhaseSettle:
		tourState.settleCnt++
		if tourState.settleCnt >= tourSettleFrames {
			tourState.phase = tourPhaseCapture
		}
	case tourPhaseCapture:
		captureScene(screenshotDir, scene)
		tourState.phase = tourPhaseAdvance
	case tourPhaseAdvance:
		tourState.sceneIdx++
		tourState.phase = tourPhaseSettle
		tourState.settleCnt = 0
		c.MemoryResetAreas()
	}
	return
}

func captureScene(dir, name string) {
	path := filepath.Join(dir, "imzrt_"+name+".png")
	if w, h, ok := imzero2env.ScreenshotSizeWH(); ok {
		c.RequestScreenshotRect(path, 0, 0, float32(w), float32(h))
	} else {
		c.RequestScreenshot(path)
	}
	log.Info().Str("path", path).Str("scene", name).Msg("imzrt tour: capture requested")
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
