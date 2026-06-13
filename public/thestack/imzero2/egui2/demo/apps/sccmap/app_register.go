package sccmap

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the static AppI descriptor every instance returns. Each
// Open yields a fresh *App with its own *treemap.Treemap, so two open
// windows have independent zoom / animation state but share the
// process-static scc tree.
var manifest = runtimeapp.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/sccmap",
	Version:  "0.1.0",
	Display:  "Repo code exploration",
	Title:    "Repo code exploration",
	Icon:     icons.PhGridNine,
	Category: "Tools",
	Surface:  runtimeapp.SurfaceWindowed,
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  styletokens.SurfaceWorkspace.W,
		PreferredHeight: styletokens.SurfaceWorkspace.H,
	},
}

func init() {
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, func() (a runtimeapp.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("sccmap: failed to register factory")
	}
}
