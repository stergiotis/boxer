package adhocdemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/sqlapplet"
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
	Summary:      "Publish ad-hoc data — rows to query in place, a tree to browse in tally",
	Icon:         icons.PhDatabase,
	Topics:       []app.TopicT{app.TopicData},
	Keywords:     []string{"dataset", "ad-hoc", "arrow", "upload", "tree", "lading"},
	Kind:         app.KindDemo,
	Surface:      app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{PreferredWidth: 900, PreferredHeight: 700},
	// The bundle publish, and what a bundle view needs (ADR-0288 §SD7) —
	// its resolve and events, and the embedded play's two escape hatches:
	// Copy in the Definition drawer and Open in Playground, which also
	// opens tally on the published tree (ADR-0222 §SD7).
	Caps: append([]app.SubjectFilter{
		{
			Pattern:   adhocdata.SubjectBundlePublish,
			Direction: app.CapDirectionPub,
			Reason:    "adhocdemo: publish and republish the computed series as an ad-hoc bundle (ADR-0288)",
		},
	}, sqlapplet.BundleViewCaps...),
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
