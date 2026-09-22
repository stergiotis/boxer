package watchbill

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/watchbill/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
)

// manifest declares the management window (ADR-0236): the watchbill client
// verbs, the task observer's and canceller's sets so the embedded task
// monitor sees the runs and can stop one from its row, and the two
// introspection tables the trail and the worker line are read from
// (ADR-0253) — one grant per table, sticky.
var manifest = app.Manifest{
	Id:       launchcfg.AppId,
	Version:  "0.1.0",
	Display:  "Watchbill",
	Title:    "Watchbill",
	Summary:  "Inspect, cancel and retry durable jobs, read their trail, see who serves them",
	Icon:     icons.PhClipboard,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"watchbill", "job", "queue", "durable", "worker", "retry", "cancel", "river"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1280,
		PreferredHeight: 800,
	},
	LaunchKind: launchcfg.Kind,
	// The split between the list and the detail, kept across the process
	// (watchbill_split.go); the host injects the persist cap for it.
	PersistedKeys: []string{splitKey},
	Caps: append(append(append(wb.ClientCaps(), task.ObserverCaps()...), task.CancelerCaps()...),
		keelsonquery.ClientCaps(wb.TableEvent, wb.TableWorker)...),
}

func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("watchbill app: failed to register factory")
	}
}
