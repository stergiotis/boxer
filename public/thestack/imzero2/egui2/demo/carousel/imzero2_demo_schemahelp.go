package demo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
)

// schemaHelpAppId is the help-library book id for the schema inspector's glyph
// reference. The inspector is a reusable widget, not a registered app, so its
// help is wired in directly via help.Register rather than through an app
// Manifest.Help — the library's documented "special wiring" path, mirroring the
// video-output book. The id doubles as the Help app's nav label: helphost falls
// back to the raw id when no manifest backs it, so it is written as the prose
// label rather than a package path.
const schemaHelpAppId = "Schema inspector"

// init registers the schema-inspector help book with the runtime help library
// so the navigator's glyph vocabulary is discoverable from the Help app.
// Failures are logged, not fatal: a missing corpus degrades to "no help for
// this widget", never a refusal to start the shell.
func init() {
	b, err := help.NewBook(schemaHelpAppId, help.MustSub(schemaview.HelpFS, "help"))
	if err != nil {
		log.Warn().Err(err).Msg("demo: schema-inspector help: NewBook failed")
		return
	}
	if err = help.Register(b); err != nil {
		log.Warn().Err(err).Msg("demo: schema-inspector help: Register failed")
	}
}
