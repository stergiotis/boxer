package splashscreen

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// assetsFS bundles the app's data files (the splash artwork and a copy of the
// project NOTICE). It is embedded as a *directory* rather than as individual
// files on purpose: the artwork assets/patronus.png is intentionally
// git-ignored (see .gitignore) and may be absent on a fresh checkout. A
// directory embed still builds as long as the directory is non-empty — the
// committed assets/NOTICE guarantees that — so a missing image degrades to the
// "image unavailable" pane at runtime instead of breaking the build.
//
// assets/NOTICE is a copy of the repo-root NOTICE, refreshed by the generate
// directive below (an embed pattern cannot escape the package directory).
//
//go:generate cp ../../NOTICE assets/NOTICE
//go:embed assets
var assetsFS embed.FS

// ManifestId is the stable AppI identity (ADR-0026: the Go import path).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/splashscreen"

var manifest = app.Manifest{
	Id:       ManifestId,
	Version:  "0.1.0",
	Display:  "Splash screen",
	Title:    "splash",
	Icon:     icons.PhSparkle,
	Category: "Tools",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		// Portrait-ish to suit the portrait artwork plus the tab strip.
		PreferredWidth:  620,
		PreferredHeight: 820,
	},
}

func init() {
	// splashscreen registers an interactive per-window App. Screenshot
	// capture is handled centrally by the widgets TestDriver via the Demos
	// registered in splashscreen_tour.go (ADR-0057).
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("splashscreen: failed to register factory")
	}
}
