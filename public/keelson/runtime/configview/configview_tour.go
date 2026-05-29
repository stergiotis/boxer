//go:build llm_generated_opus47

// Screenshot tour for configview. Mirrors leewaywidgets_tour:
// deterministic 3-phase state machine (settle → capture → advance)
// that walks one scene per visual mode and writes a PNG per scene
// to $IMZERO2_SCREENSHOT_DIR. AnimationFreeze + MemoryResetAreas
// between scenes keep CollapsingHeader state pixel-stable.

package configview

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
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

// tourScene names one capture and a setup hook that pins the
// tourApp's filter / expansion state. The dense category list +
// one expanded category cover the two visual modes the operator
// actually sees in interactive use.
type tourScene struct {
	Name  string
	Setup func(inst *App)
}

// Single scene — egui persists CollapsingHeader open state across
// frames, so a "closed → expanded" two-scene tour would render
// identically once any header has been opened. The single expanded
// view exercises every visual we care about (status dot, type chip,
// lock icon on the sensitive var, value mask, CLI flag chip), so a
// one-scene capture is sufficient for visual regression coverage.
var tourScenes = []tourScene{
	{
		Name: "expanded",
		Setup: func(inst *App) {
			inst.filter = Filter{}
			inst.expandedCat = env.CategoryDatabase
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

	tourApp *App
}

var tourState = tourStateS{sceneAppliedFor: -1}

// RenderLoopHandlerTour is the screenshot-tour entry point. The
// app_register screenshot-mode factory routes Frame() through this
// via a SeededFuncApp. Walks tourScenes in order, captures each as
// configview_<scene>.png, and exits the viewport after the last
// scene.
func RenderLoopHandlerTour(seed uint64) (err error) {
	screenshotDir := imzero2env.ScreenshotDir.Get()
	if screenshotDir == "" {
		return
	}
	if !tourState.setupDone {
		err = os.MkdirAll(screenshotDir, 0o755)
		if err != nil {
			log.Warn().Err(err).Str("dir", screenshotDir).Msg("configview tour: unable to create output dir")
			err = nil
		}
		c.SetAnimationFreeze(true)
		c.MemoryResetAreas()
		log.Info().Int("scenes", len(tourScenes)).Str("dir", screenshotDir).Msg("configview tour: starting")
		tourState.setupDone = true
	}
	c.RequestRepaint()

	if tourState.doneAll {
		return
	}
	if int(tourState.sceneIdx) >= len(tourScenes) {
		log.Info().Str("dir", screenshotDir).Msg("configview tour: complete")
		tourState.doneAll = true
		c.ContextSendViewPortCommandClose()
		return
	}

	if tourState.tourApp == nil {
		tourState.tourApp = newInstance(manifest)
	}

	scene := &tourScenes[tourState.sceneIdx]
	if tourState.sceneAppliedFor != tourState.sceneIdx {
		scene.Setup(tourState.tourApp)
		tourState.sceneAppliedFor = tourState.sceneIdx
		// CollapsingHeader open state is persisted in egui Memory;
		// resetting Areas here makes the per-scene DefaultOpen
		// recommendation actually take effect.
		c.MemoryResetAreas()
	}

	_ = tourState.tourApp.Frame(nil)
	_ = seed

	switch tourState.phase {
	case tourPhaseSettle:
		tourState.settleCnt++
		if tourState.settleCnt >= tourSettleFrames {
			tourState.phase = tourPhaseCapture
		}
	case tourPhaseCapture:
		path := filepath.Join(screenshotDir, "configview_"+scene.Name+".png")
		c.RequestScreenshot(path)
		log.Info().Str("path", path).Str("scene", scene.Name).Msg("configview tour: capture requested")
		tourState.phase = tourPhaseAdvance
	case tourPhaseAdvance:
		tourState.sceneIdx++
		tourState.phase = tourPhaseSettle
		tourState.settleCnt = 0
	}
	return
}
