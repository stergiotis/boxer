package appstate

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	as "github.com/stergiotis/boxer/public/keelson/runtime/appstate"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// AppId is the window's durable identity.
const AppId app.AppIdT = "github.com/stergiotis/boxer/apps/appstate"

// manifest declares the manager window (ADR-0185 §SD6): the delete seam's
// client set, and nothing else — reading is the introspection endpoint.
// The capability is not sticky, so the broker asks on every Mount.
var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "App state",
	Title:    "App state",
	Summary:  "See what each app keeps across restarts, and clear it",
	Icon:     icons.PhBroom,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"app state", "persist", "workingset", "column width", "forget", "clear", "storage"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1100,
		PreferredHeight: 700,
	},
	Caps: as.ClientCaps(),
}

func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("appstate app: failed to register factory")
	}
}
