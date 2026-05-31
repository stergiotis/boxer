package godepview

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

//go:embed help
var helpFS embed.FS

// manifest is the static AppI descriptor every godepview instance returns.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/godepview",
	Version:  "0.1.0",
	Display:  "Go dependency explorer",
	Title:    "Go Dependency Explorer",
	Icon:     "🕸",
	Category: "Tools",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1200,
		PreferredHeight: 760,
	},
	Help: help.MustSub(helpFS, "help"),
}

func init() {
	// Factory registration. The ctor is the composition root (ADR-0064
	// SD3): the one place that names the concrete collector, wiring a
	// LiveCollector into the App's godep.SourceI port. The render path
	// (godepview.go / godepview_view.go) never imports godepcollect.
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp(godepcollect.New(resolveCollectorConfig()))
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("godepview: failed to register factory")
	}
}
