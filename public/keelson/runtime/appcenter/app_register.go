package appcenter

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appcenter/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
)

// AppId is the window's durable identity.
const AppId app.AppIdT = launchcfg.AppId

// manifest declares the app center (ADR-0260): one sticky read grant per
// introspection table a page is assembled from (ADR-0253 §SD1), and the
// open request its Open buttons send. It reads; it changes nothing, but for
// Measure CPU (ADR-0351 §SD3), a task of the window that publishes the
// profile it takes as an ad-hoc dataset.
var manifest = app.Manifest{
	Id:       AppId,
	Version:  "0.1.0",
	Display:  "App center",
	Title:    "App center",
	Summary:  "Read one page per app, and what each open window costs and runs",
	Icon:     icons.PhSquaresFour,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"app", "inspect", "capability", "state", "coverage", "adr", "log", "run", "history", "running", "window", "task", "frame time"},
	Surface:  app.SurfaceWindowed,
	Maturity: app.MaturitySupported,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1200,
		PreferredHeight: 800,
	},
	LaunchKind: launchcfg.Kind,
	// The operations catalog (ADR-0345), in appcenter_ops.go.
	Operations: ops.Catalog(),
	Caps:       append(append(keelsonquery.ClientCaps(readTables...), windowopen.OpenCap("app center: Open — open the app a page shows, the app-state manager, or play on a section's table"), adhocdata.PublishCap("app center: Measure CPU — publish the profile it takes, for Explore (ADR-0351 §SD4)")), task.ProducerCaps()...),
}

func init() {
	app.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
}
