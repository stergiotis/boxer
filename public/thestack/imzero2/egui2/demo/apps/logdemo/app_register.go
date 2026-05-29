//go:build llm_generated_opus47

package logdemo

import (
	"github.com/rs/zerolog/log"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the static AppI descriptor every instance returns. Kept
// package-level so the factory ctor can hand a fresh instance back
// without re-running Manifest validation.
var manifest = runtimeapp.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo",
	Version:  "0.1.0",
	Display:  "Log emitter",
	Title:    "Log emitter",
	Icon:     icons.PhPaperPlaneTilt,
	Category: "Demos",
	Surface:  runtimeapp.SurfaceWindowed,
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  720,
		PreferredHeight: 280,
	},
}

func init() {
	// Factory registration — each Open() yields a fresh *App with its
	// own emit counter, stream toggle, and custom-message buffer. The
	// per-instance number stamped on the logger lets the logviewer
	// tell two open windows apart in its tail table.
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, func() (a runtimeapp.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("logdemo: failed to register factory")
	}
}
