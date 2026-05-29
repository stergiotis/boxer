//go:build llm_generated_opus47

package taskdemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// manifest declares the demo's identity + the single Caps entry the task
// primitive requires. task.ProducerCaps() expands to task.> with
// CapDirectionBoth — covering both the publish side (created / progress
// / done / error) and the subscribe side (per-task cancel inbox + the
// observer WatchAll subscription on task.>). A separate ObserverCaps
// is not needed because Both already includes Sub.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/taskdemo",
	Version:  "0.1.0",
	Display:  "Task primitive",
	Title:    "Background task",
	Icon:     icons.PhHourglass,
	Category: "Demos",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  720,
		PreferredHeight: 520,
	},
	Caps: task.ProducerCaps(),
}

// init registers the demo into app.DefaultRegistry. Factory ctor so each
// open window owns its own observer state.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("taskdemo: failed to register factory")
	}
}
