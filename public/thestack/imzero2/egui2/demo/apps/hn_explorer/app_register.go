//go:build llm_generated_opus47

package hn_explorer

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// manifest is the static AppI descriptor every instance returns. Kept
// package-level so the factory ctor can hand a fresh instance back
// without re-running Manifest validation.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/hn_explorer",
	Version:  "0.1.0",
	Display:  "Hacker News explorer",
	Title:    "HN Arrow Explorer",
	Icon:     "🗞",
	Category: "Tools",
	Surface:  app.SurfaceWindowed,
}

func init() {
	// Factory registration — each Open() yields a fresh *App with its
	// own filter selections, sort mode, current view, and row
	// selection. The shared arrowStore caches ClickHouse rows so the
	// N-windows-N-fetches thundering-herd never happens.
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("hn_explorer: failed to register factory")
	}
}
