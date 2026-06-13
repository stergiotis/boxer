package capinspector

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

//go:embed help
var helpFS embed.FS

// ManifestId is exported so the carousel can call host.Open with a
// stable identifier when a status-bar segment is clicked.
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/capinspector"

var manifest = app.Manifest{
	Id:       ManifestId,
	Version:  "0.1.0",
	Display:  "Capability inspector",
	Title:    "Capability inspector",
	// Phosphor plug — the capability-inspector metaphor (plugged-in
	// runtime introspection); rendered from the Phosphor icon font
	// registered at carousel startup (ADR-0044).
	Icon:     icons.PhPlug,
	Category: "Runtime",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  860,
		PreferredHeight: 640,
	},
	// No declared Caps yet — the inspector reads app.DefaultRegistry
	// in-process; a future Phase 2 may add `runtime.inspect.>` for
	// cross-process queries.
	Help: help.MustSub(helpFS, "help"),
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("capinspector: failed to register factory")
	}
}
