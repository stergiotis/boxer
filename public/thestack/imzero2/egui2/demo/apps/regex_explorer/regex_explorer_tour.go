//go:build llm_generated_opus47

package regex_explorer

// Screenshot tour for the regex explorer. Mirrors widgets.RenderLoopHandlerTestDriver
// (public/thestack/imzero2/egui2/demo/apps/widgets/testdriver.go): a deterministic
// 3-phase state machine — settle → capture → advance — that walks a fixed list of
// scenes and writes one PNG per scene to $IMZERO2_SCREENSHOT_DIR. Animations are
// frozen and area memory is reset between scenes for pixel-stable captures.

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

const tourSettleFrames int32 = 8

type tourPhaseE uint8

const (
	tourPhaseSettle  tourPhaseE = 0
	tourPhaseCapture tourPhaseE = 1
	tourPhaseAdvance tourPhaseE = 2
)

// tourScene is one capture in the regex_explorer tour: a name (used as the PNG
// filename stem) and a setup hook that mutates the package-global [App] before
// the first settle frame of the scene.
type tourScene struct {
	Name  string
	Setup func()
}

// tourScenes is the fixed list of captures the tour produces. Kept small and
// purely visual — the tour is for layout regression detection, not for
// exercising the ClickHouse pipeline.
var tourScenes = []tourScene{
	{
		Name: "empty",
		Setup: func() {
			app.mu.Lock()
			app.pattern = ""
			app.haystack = ""
			app.patternList = ""
			app.replacement = ""
			app.mu.Unlock()
		},
	},
	{
		Name: "populated",
		Setup: func() {
			app.mu.Lock()
			app.pattern = `\w+`
			app.haystack = "hello world 123"
			app.patternList = ""
			app.replacement = ""
			app.mu.Unlock()
		},
	},
}

type tourStateS struct {
	setupDone       bool
	doneAll         bool
	sceneIdx        int32
	sceneAppliedFor int32
	phase           tourPhaseE
	settleCnt       int32
}

var tourState = tourStateS{sceneAppliedFor: -1}

// RenderLoopHandlerTour is the screenshot-tour entry point for the regex
// explorer. Activates when IMZERO2_SCREENSHOT_DIR is set; the demo dispatcher
// in imzero2_demo_resolve.go selects this handler over [RenderLoopHandlerDemo].
//
// Each frame: applies the current scene's setup once, renders the regex
// explorer window, then advances the settle/capture/advance state machine.
// Captures the full viewport via [c.RequestScreenshot] — the regex explorer
// uses a floating Window so a fixed screenshot rect is not meaningful. The
// seed is the per-instance SeededFuncApp value that scopes widget ids
// under a private parent (multi-instance safety).
func RenderLoopHandlerTour(seed uint64) (err error) {
	screenshotDir := imzero2env.ScreenshotDir.Get()
	if screenshotDir == "" {
		return
	}
	// Honor IMZERO2_SCREENSHOT_DETERMINISTIC — the regex_explorer tour
	// scans a synthetic corpus whose byte output isn't stable across
	// runs (scan iteration order, internal state). Skip the whole tour
	// when the deterministic gate is set; default mode (empty env var)
	// still captures the two scenes as before.
	if imzero2env.ScreenshotDeterministic.Get() != "" {
		if !tourState.doneAll {
			log.Info().Str("dir", screenshotDir).Msg("regex_explorer tour: skipped (IMZERO2_SCREENSHOT_DETERMINISTIC set)")
			tourState.doneAll = true
		}
		return
	}
	if !tourState.setupDone {
		err = os.MkdirAll(screenshotDir, 0o755)
		if err != nil {
			log.Warn().Err(err).Str("dir", screenshotDir).Msg("regex_explorer tour: unable to create output dir")
			err = nil
		}
		c.SetAnimationFreeze(true)
		c.MemoryResetAreas()
		log.Info().Int("scenes", len(tourScenes)).Str("dir", screenshotDir).Msg("regex_explorer tour: starting")
		tourState.setupDone = true
	}
	c.RequestRepaint()

	if tourState.doneAll {
		return
	}
	if int(tourState.sceneIdx) >= len(tourScenes) {
		log.Info().Str("dir", screenshotDir).Msg("regex_explorer tour: complete")
		tourState.doneAll = true
		return
	}

	scene := &tourScenes[tourState.sceneIdx]
	if tourState.sceneAppliedFor != tourState.sceneIdx {
		scene.Setup()
		tourState.sceneAppliedFor = tourState.sceneIdx
	}

	app.ids.Reset()
	for range c.IdScope(app.ids.PrepareSeq(seed)) {
		RenderWindow()
	}

	switch tourState.phase {
	case tourPhaseSettle:
		tourState.settleCnt++
		if tourState.settleCnt >= tourSettleFrames {
			tourState.phase = tourPhaseCapture
		}
	case tourPhaseCapture:
		path := filepath.Join(screenshotDir, "regex_explorer_"+scene.Name+".png")
		// IMZERO2_SCREENSHOT_SIZE narrows the capture to a fixed WxH
		// rect (matching the widgets TestDriver and imztop tour).
		// hmi.sh resizes the eframe viewport to the same WxH so the
		// regex explorer window fills the rect exactly. Empty /
		// malformed env → keep the legacy full-viewport capture.
		// ADR-0008 SD5.
		if w, h, ok := imzero2env.ScreenshotSizeWH(); ok {
			c.RequestScreenshotRect(path, 0, 0, float32(w), float32(h))
		} else {
			c.RequestScreenshot(path)
		}
		log.Info().Str("path", path).Str("scene", scene.Name).Msg("regex_explorer tour: capture requested")
		tourState.phase = tourPhaseAdvance
	case tourPhaseAdvance:
		tourState.sceneIdx++
		tourState.phase = tourPhaseSettle
		tourState.settleCnt = 0
		c.MemoryResetAreas()
	}
	return
}
