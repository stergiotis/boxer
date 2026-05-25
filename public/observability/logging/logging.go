package logging

import (
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// init installs eh.MarshalError as the zerolog ErrorMarshalFunc before any
// importer's init() runs, so a stray log.X().Err(...) during package
// initialisation is still marshalled through the eh framework. The writer,
// global level, correlation id, and startup-info emission are configured
// later by Apply, which the boxer cli.App invokes from its Before hook.
func init() {
	zerolog.ErrorMarshalFunc = eh.MarshalError
}
