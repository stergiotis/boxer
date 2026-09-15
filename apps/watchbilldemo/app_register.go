package watchbilldemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
)

// manifest declares the demo and the two capability sets it holds: the
// watchbill client verbs (ADR-0234 §SD1) and the task producer's set, which
// the task monitor needs to observe the runs and to cancel one from its
// row — every job run is a keelson task with the job's id (ADR-0223 §SD5).
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/watchbilldemo",
	Version:  "0.1.0",
	Display:  "Watchbill",
	Title:    "Durable job",
	Summary:  "Enqueue a job on the watchbill and watch a worker take it",
	Icon:     icons.PhClipboardText,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"watchbill", "job", "queue", "durable", "worker", "retry"},
	Kind:     app.KindDemo,
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  900,
		PreferredHeight: 620,
	},
	Caps: append(watchbill.ClientCaps(), task.ProducerCaps()...),
}

// init registers the app and the handler for its kind. The handler is
// process-wide: any worker in a binary that links this package drains
// demo.sleep jobs, whether or not a window is open (ADR-0223 §SD8).
func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("watchbilldemo: failed to register factory")
	}
	if err := watchbill.Register(sleepHandler{}); err != nil {
		log.Warn().Err(err).Msg("watchbilldemo: failed to register handler")
	}
}
