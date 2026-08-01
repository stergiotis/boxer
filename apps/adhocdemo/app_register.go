package adhocdemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// ManifestId is this app's identity — its Go import path (ADR-0026 id rule).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/adhocdemo"

var manifest = app.Manifest{
	Id:           ManifestId,
	Version:      "0.1.0",
	Display:      "Ad-hoc dataset demo",
	Title:        "Ad-hoc dataset demo",
	Icon:         icons.PhDatabase,
	Topics:       []app.TopicT{app.TopicData},
	Keywords:     []string{"dataset", "ad-hoc", "arrow", "upload"},
	Kind:         app.KindDemo,
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
		// The embedded applet's two escape hatches ride this manifest
		// (ADR-0132 §SD8: an embedded applet's capabilities are the
		// embedder's). Without them the surface still offers both — the
		// Definition drawer's per-fence Copy no-ops silently, Open in
		// Playground shows a permission refusal — so the caps are what makes
		// it honest.
		{
			Pattern:   clipboardbroker.SubjectWrite,
			Direction: app.CapDirectionPub,
			Reason:    "adhocdemo: copy a fenced block out of the embedded applet's Definition drawer (ADR-0132 §SD3)",
		},
		{
			Pattern:   windowhost.OpenSubject,
			Direction: app.CapDirectionPub,
			Reason:    "adhocdemo: Open in Playground — reopen the applet buffer in a full play window (ADR-0135 §SD7)",
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
