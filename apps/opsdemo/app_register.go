// Package opsdemo is the smallest participant in the app operations contract
// (ADR-0269): a note and a level, each bound to a widget and each a resource
// an agent can read and write through the app's catalog while the person
// edits the same widgets. It exists to watch the two writers meet — which
// change wins a tie, when a command waits, what the person sees.
package opsdemo

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// helpFS embeds the inline help (apps/opsdemo/help/*.md), indexed by the
// help library and shown by the HelpHost on F1 (ADR-0047).
//
//go:embed help/*.md
var helpFS embed.FS

// AppId is the demo's manifest id.
const AppId app.AppIdT = "github.com/stergiotis/boxer/apps/opsdemo"

var manifest = app.Manifest{
	Id:         AppId,
	Version:    "0.1.0",
	Display:    "Operations demo",
	Summary:    "Edit a note an agent can edit too, and watch the two meet",
	Icon:       icons.PhHandshake,
	Topics:     []app.TopicT{app.TopicRuntime},
	Keywords:   []string{"agent", "operations", "commands", "queries", "adr-0269"},
	Kind:       app.KindDemo,
	Surface:    app.SurfaceWindowed,
	Operations: ops.Catalog(),
	Help:       help.MustSub(helpFS, "help"),
	Caps: []app.SubjectFilter{{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub,
		Reason: "opsdemo: copy the note to the clipboard"}},
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  520,
		PreferredHeight: 360,
	},
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("opsdemo: failed to register factory")
	}
}
