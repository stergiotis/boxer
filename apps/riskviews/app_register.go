package riskviews

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/riskviews",
	Version:  "0.1.0",
	Display:  "Risk views",
	Title:    "Risk view prototypes",
	Summary:  "Nine risk-management views over one synthetic register, drawn with imzero2 widgets",
	Icon:     icons.PhChartBar,
	Topics:   []app.TopicT{app.TopicUi, app.TopicData},
	Keywords: []string{"risk", "matrix", "loss exceedance", "bow-tie", "kri", "calibration"},
	Kind:     app.KindDemo,
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1280,
		PreferredHeight: 900,
	},
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("riskviews: failed to register factory")
	}
}
