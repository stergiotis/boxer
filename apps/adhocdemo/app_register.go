package adhocdemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// ManifestId is this app's identity — its Go import path (ADR-0026 id rule).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/adhocdemo"

var manifest = app.Manifest{
	Id:           ManifestId,
	Version:      "0.1.0",
	Display:      "Ad-hoc dataset demo",
	Title:        "Ad-hoc dataset demo",
	Icon:         icons.PhDatabase,
	Category:     "Runtime",
	Surface:      app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{PreferredWidth: 900, PreferredHeight: 700},
	Caps: []app.SubjectFilter{
		{
			Pattern:   adhocdata.SubjectPublish,
			Direction: app.CapDirectionPub,
			Reason:    "adhocdemo: publish and republish an ephemeral dataset the embedded applet queries (ADR-0134)",
		},
		{
			Pattern:   adhocdata.SubjectRetract,
			Direction: app.CapDirectionPub,
			Reason:    "adhocdemo: retract the dataset when the window closes",
		},
	},
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = &App{}
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("adhocdemo: failed to register factory")
	}
}
