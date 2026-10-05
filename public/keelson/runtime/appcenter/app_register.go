package appcenter

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appcenter/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// AppId is the window's durable identity.
const AppId app.AppIdT = launchcfg.AppId

// manifest declares the app center (ADR-0260): one sticky read grant per
// introspection table a page is assembled from (ADR-0253 §SD1), and the
// open request its Open buttons send. It reads; it changes nothing.
var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "App center",
	Title:    "App center",
	Summary:  "One page per app: what it did, may do, keeps, cites and covers",
	Icon:     icons.PhSquaresFour,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"app", "inspect", "capability", "state", "coverage", "adr", "log", "run", "history"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1200,
		PreferredHeight: 800,
	},
	LaunchKind: launchcfg.Kind,
	Caps: append(keelsonquery.ClientCaps(readTables...), app.SubjectFilter{
		Pattern:   windowhost.OpenSubject,
		Direction: app.CapDirectionPub,
		Reason:    "app center: Open — open the app a page shows, the app-state manager, or play on a section's table",
	}),
}

func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("appcenter app: failed to register factory")
	}
}
