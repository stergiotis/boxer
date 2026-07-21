package sqlappletcreator

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
)

// manifest is the creator's identity and attenuation. Windowed, factory-backed
// (each Open is a fresh App so config-carrying opens and multiple windows work
// — ADR-0135's singleton-refusal note). Two output capabilities (ADR-0132 O4
// "A+B"): publish the composed document to the applet store (mint), and the fs
// Powerbox save dialog + granted write handle (export to a user-chosen file).
// LaunchKind declares the config the playground hands it (appletcreatecfg.Kind).
var manifest = app.Manifest{
	Id:           appletcreatecfg.AppId,
	Version:      "0.1.0",
	Display:      "SQL applet creator",
	Title:        "SQL applet creator",
	Icon:         "🧩",
	Category:     "Tools",
	Surface:      app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{PreferredWidth: 640, PreferredHeight: 560},
	Caps: []app.SubjectFilter{
		{
			Pattern:   appletstore.SubjectSave,
			Direction: app.CapDirectionPub,
			Reason:    "submit the authored buffer to the applet store (ADR-0132 O4 'A')",
		},
		{
			Pattern:   fsbroker.SubjectDialogWrite,
			Direction: app.CapDirectionPub,
			Reason:    "Export as .md via the Powerbox save dialog (ADR-0132 O4 'B')",
		},
		{
			Pattern:   fsbroker.HandleSubjectPrefix + ">",
			Direction: app.CapDirectionPub,
			Reason:    "write the composed document through the granted file handle",
		},
	},
	LaunchKind: appletcreatecfg.Kind,
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = &App{}
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("sqlappletcreator: failed to register factory")
	}
}
