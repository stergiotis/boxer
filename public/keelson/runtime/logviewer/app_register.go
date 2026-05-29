//go:build llm_generated_opus47

package logviewer

import (
	"embed"

	"github.com/rs/zerolog/log"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

//go:embed help
var helpFS embed.FS

// manifest is the static AppI descriptor every LogViewerApp instance
// returns. Kept package-level so the factory ctor can hand a fresh
// instance back without re-running Manifest validation.
var manifest = runtimeapp.Manifest{
	Id:       "github.com/stergiotis/boxer/public/keelson/runtime/logviewer",
	Version:  "0.1.0",
	Display:  "Log viewer",
	Title:    "Log Viewer",
	Icon:     "📜",
	Category: "Runtime",
	Surface:  runtimeapp.SurfaceWindowed,
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  1100,
		PreferredHeight: 600,
	},
	Help: help.MustSub(helpFS, "help"),
}

func init() {
	// Factory registration — each dock-host Open() yields a fresh
	// LogViewerApp with its own filter state and a unique per-instance
	// seed so two open tiles produce disjoint Go-side widget IDs.
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, func() (a runtimeapp.AppI, ctorErr error) {
		a = newInstance(manifest)
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("logviewer: failed to register factory")
	}
}
