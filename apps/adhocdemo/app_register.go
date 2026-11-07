package adhocdemo

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// helpFS embeds the help book (apps/adhocdemo/help/*.md): F1 opens it for
// the person, and the help service serves the same page to a model
// (ADR-0047, keelson-app A4).
//
//go:embed help/*.md
var helpFS embed.FS

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
	Caps:       adhocCaps(),
	Operations: adhocOps.Catalog(),
	Help:       help.MustSub(helpFS, "help"),
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

// adhocCaps composes the manifest's capabilities from the helpers that
// own them (S3): the publish, the task producer's (the publishes run as
// keelson tasks owned by the window; the helper's reason is the
// declaration), and the bundle view's.
func adhocCaps() (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{{
		Pattern:   adhocdata.SubjectBundlePublish,
		Direction: app.CapDirectionPub,
		Reason:    "adhocdemo: publish and republish the computed series as an ad-hoc bundle (ADR-0288)",
	}}
	caps = append(caps, task.ProducerCaps()...)
	caps = append(caps, sqlapplet.BundleViewCaps...)
	return
}
