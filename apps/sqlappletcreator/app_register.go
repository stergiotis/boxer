package sqlappletcreator

import (
	"embed"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"

	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

//go:embed help
var helpFS embed.FS

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
	Summary:      "Author a SQL applet document and publish it to the store",
	Icon:         icons.PhPuzzlePiece,
	Topics:       []app.TopicT{app.TopicSql},
	Keywords:     []string{"applet", "author", "publish", "save"},
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
		// Save and Export run as keelson tasks owned by the window.
		task.ProducerCaps()[0],
	},
	LaunchKind: appletcreatecfg.Kind,
	Help:       help.MustSub(helpFS, "help"),
	Operations: ops.Catalog(),
}

// DraftSeed opens a new window on a draft of the caller's choosing (ADR-0009
// seed variable, the BOXER_MDEDIT_DOC shape): a scene needs the four fields
// filled to read them back, and the launch config carries only the buffer
// and its endpoint. The value is JSON with the launch config's Sql and
// Endpoint and the draft's Slug, Title, Summary and Icon; the buffer and the
// endpoint go through the launch config's own path. A caller's launch config
// wins over it.
var DraftSeed = env.NewString(env.Spec{
	Name:        "BOXER_SQLAPPLETCREATOR_DRAFT",
	Description: "a draft for a new SQL applet creator window, as JSON with Sql and Endpoint (as the launch config carries them) and Slug, Title, Summary and Icon; a caller's launch config wins over it; for scenes and demos",
	Category:    env.CategoryE("boxer-sqlappletcreator"),
})

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = &App{}
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("sqlappletcreator: failed to register factory")
	}
}
