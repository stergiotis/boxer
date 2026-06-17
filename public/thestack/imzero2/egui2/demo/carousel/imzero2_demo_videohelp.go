package demo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/videooutput"
)

// videoHelpAppId is the help-library book id for the video-output control's
// readout reference (ADR-0088). The control is carousel chrome — a status-bar
// indicator plus a floating settings window, not a registered app — so its
// help is wired in directly via help.Register rather than through an app
// Manifest.Help. That is the library's documented "special wiring" path. The
// id doubles as the Help app's nav label: helphost falls back to the raw id
// when no manifest backs it, so it is written as the prose label rather than
// a package path.
const videoHelpAppId = "Video output"

// init registers the video-output help book with the runtime help library so
// the panel's readouts are discoverable from the Help app. Failures are
// logged, not fatal: a missing corpus degrades to "no help for this control",
// never a refusal to start the shell.
func init() {
	b, err := help.NewBook(videoHelpAppId, help.MustSub(videooutput.HelpFS, "help"))
	if err != nil {
		log.Warn().Err(err).Msg("demo: video-output help: NewBook failed")
		return
	}
	if err = help.Register(b); err != nil {
		log.Warn().Err(err).Msg("demo: video-output help: Register failed")
	}
}
