package demo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
)

// introspectHelpBookId is the help-library book id for the keelson
// introspection facility (ADR-0094). The endpoint is runtime chrome, not a
// registered app, so its help is wired in directly via help.Register rather
// than through an app Manifest.Help — the library's documented "special
// wiring" path. The id doubles as the Help app's nav label.
const introspectHelpBookId = "Introspection tables"

// init registers the introspection help book with the runtime help library so
// the endpoint and its tables are discoverable from the Help app. Failures are
// logged, not fatal: a missing corpus degrades to "no help", never a refusal
// to start the shell.
func init() {
	b, err := help.NewBook(introspectHelpBookId, help.MustSub(introspecthttp.HelpFS, "help"))
	if err != nil {
		log.Warn().Err(err).Msg("demo: introspection help: NewBook failed")
		return
	}
	if err = help.Register(b); err != nil {
		log.Warn().Err(err).Msg("demo: introspection help: Register failed")
	}
}
