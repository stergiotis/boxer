package widgets

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// manifest is the static AppI descriptor every instance returns. Kept
// package-level so the factory ctor can hand a fresh instance back
// without re-running Manifest validation.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets",
	Version:  "0.1.0",
	Display:  "Widget gallery",
	Title:    "Widget gallery",
	Summary:  "Browse every registered widget demo with its source",
	Icon:     icons.PhSquaresFour,
	Topics:   []app.TopicT{app.TopicUi},
	Keywords: []string{"gallery", "widgets", "showcase", "demo"},
	Kind:     app.KindDemo,
	Surface:  app.SurfaceWindowed,
	Caps: append([]app.SubjectFilter{
		{
			Pattern:   chlocalbroker.SubjectExecPrefix + timerangepicker.PoolName,
			Direction: app.CapDirectionPub,
			Reason:    "evaluate user time-range expressions (ADR-0016 Phase 4)",
		},
	},
		// Demos that run background jobs as keelson tasks (ADR-0038): the
		// waveform player's peaks build, the distsummary band warm-up.
		task.ProducerCaps()...),
}

// init registers the widget-showcase app. Interactive mode hands back
// a fresh per-instance *App per Open so each window has its own
// gallery filter selection. Tour mode (IMZERO2_SCREENSHOT_DIR set)
// uses SeededFuncApp — tours are single-instance by design.
func init() {
	var ctor app.AppCtor
	if imzero2env.ScreenshotDir.Get() != "" {
		ctor = func() (a app.AppI, err error) {
			a, err = app.NewSeededFuncApp(manifest, RenderLoopHandlerTestDriver)
			return
		}
	} else {
		ctor = func() (a app.AppI, err error) {
			a = newApp()
			return
		}
	}
	err := app.DefaultRegistry.RegisterFactory(manifest, ctor)
	if err != nil {
		log.Warn().Err(err).Msg("widgets: failed to register factory")
	}
}
