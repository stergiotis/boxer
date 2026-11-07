package splashscreen

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// assetsFS bundles the app's data files: the splash artwork
// (assets/splash.png, tracked) and a copy of the project NOTICE. It is
// embedded as a directory so the two travel as one pattern; a read or
// decode failure of the artwork still degrades to the "image unavailable"
// pane at runtime rather than failing the build.
//
// assets/NOTICE is a copy of the repo-root NOTICE, refreshed by the generate
// directive below (an embed pattern cannot escape the package directory).
//
//go:generate cp ../../NOTICE assets/NOTICE
//go:embed assets
var assetsFS embed.FS

// helpFS embeds the help book's static page (help/overview.md); the About
// content is appended on open (splashscreen_help.go).
//
//go:embed help/*.md
var helpFS embed.FS

// ManifestId is the stable AppI identity (ADR-0026: the Go import path).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/splashscreen"

var manifest = app.Manifest{
	Id:       ManifestId,
	Version:  "0.1.0",
	Display:  "Splash screen",
	Title:    "splash",
	Summary:  "Show the build, version and third-party licence notices",
	Icon:     icons.PhSparkle,
	Topics:   []app.TopicT{app.TopicAbout},
	Keywords: []string{"about", "licence", "license", "notice", "version"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		// Portrait-ish to suit the portrait artwork plus the tab strip.
		PreferredWidth:  620,
		PreferredHeight: 820,
	},
	Help: newHelpFS(),
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
