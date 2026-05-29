//go:build llm_generated_opus47

package leewaywidgets_demo

// Screenshot tour for the leewaywidgets showcase. Mirrors regex_explorer_tour
// (public/thestack/imzero2/egui2/demo/apps/regex_explorer/regex_explorer_tour.go):
// a deterministic 3-phase state machine — settle → capture → advance — that
// walks one scene per view (table2 / json / schema / fixture) and writes a
// PNG per scene to $IMZERO2_SCREENSHOT_DIR. Animations are frozen and area
// memory is reset between scenes for pixel-stable captures.

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const tourSettleFrames int32 = 8

type tourPhaseE uint8

const (
	tourPhaseSettle  tourPhaseE = 0
	tourPhaseCapture tourPhaseE = 1
	tourPhaseAdvance tourPhaseE = 2
)

// tourScene is one capture: a name (used as the PNG filename stem) and a
// setup hook that pins the tour App's selectedView for the scene.
type tourScene struct {
	Name  string
	Setup func(inst *App)
}

var tourScenes = []tourScene{
	{Name: "table2", Setup: func(inst *App) { inst.selectedView = viewKeyTable2 }},
	{Name: "json", Setup: func(inst *App) { inst.selectedView = viewKeyJSON }},
	{Name: "schema", Setup: func(inst *App) { inst.selectedView = viewKeySchemaGo }},
	{Name: "fixture", Setup: func(inst *App) { inst.selectedView = viewKeyFixtureGo }},
}

type tourStateS struct {
	setupDone       bool
	doneAll         bool
	sceneIdx        int32
	sceneAppliedFor int32
	phase           tourPhaseE
	settleCnt       int32

	// tourApp is the App instance the tour renders through. Allocated
	// on first use so the renderer methods have a receiver; the scene
	// setup hooks mutate tourApp.selectedView between captures.
	tourApp *App
}

var tourState = tourStateS{sceneAppliedFor: -1}

// RenderLoopHandlerTour is the screenshot-tour entry point for the
// leewaywidgets showcase. The demo dispatcher activates this handler when
// IMZERO2_SCREENSHOT_DIR is set (see imzero2_demo_resolve.go case 4).
//
// One scene per view (table2 / json / schema / fixture) is captured.
// Each scene gets tourSettleFrames of layout time before capture so cell
// measurements are stable. The seed comes from SeededFuncApp — see
// [RenderLoopHandlerDemo] for the multi-instance rationale.
func RenderLoopHandlerTour(seed uint64) (err error) {
	screenshotDir := imzero2env.ScreenshotDir.Get()
	if screenshotDir == "" {
		return
	}
	if !tourState.setupDone {
		err = os.MkdirAll(screenshotDir, 0o755)
		if err != nil {
			log.Warn().Err(err).Str("dir", screenshotDir).Msg("leewaywidgets tour: unable to create output dir")
			err = nil
		}
		c.SetAnimationFreeze(true)
		c.MemoryResetAreas()
		log.Info().Int("scenes", len(tourScenes)).Str("dir", screenshotDir).Msg("leewaywidgets tour: starting")
		tourState.setupDone = true
	}
	c.RequestRepaint()

	if tourState.doneAll {
		return
	}
	if int(tourState.sceneIdx) >= len(tourScenes) {
		log.Info().Str("dir", screenshotDir).Msg("leewaywidgets tour: complete")
		tourState.doneAll = true
		c.ContextSendViewPortCommandClose()
		return
	}

	if tourState.tourApp == nil {
		tourState.tourApp = newApp()
	}

	scene := &tourScenes[tourState.sceneIdx]
	if tourState.sceneAppliedFor != tourState.sceneIdx {
		scene.Setup(tourState.tourApp)
		tourState.sceneAppliedFor = tourState.sceneIdx
	}

	// Reuse the App's Frame so layout matches interactive mode exactly.
	// The seed parameter is ignored here — tourApp owns its own seed,
	// which is what its widget ids derive from.
	_ = tourState.tourApp.Frame(nil)
	_ = seed

	switch tourState.phase {
	case tourPhaseSettle:
		tourState.settleCnt++
		if tourState.settleCnt >= tourSettleFrames {
			tourState.phase = tourPhaseCapture
		}
	case tourPhaseCapture:
		path := filepath.Join(screenshotDir, "leewaywidgets_"+scene.Name+".png")
		c.RequestScreenshot(path)
		log.Info().Str("path", path).Str("scene", scene.Name).Msg("leewaywidgets tour: capture requested")
		tourState.phase = tourPhaseAdvance
	case tourPhaseAdvance:
		tourState.sceneIdx++
		tourState.phase = tourPhaseSettle
		tourState.settleCnt = 0
		c.MemoryResetAreas()
	}
	return
}
