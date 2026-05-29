//go:build llm_generated_opus47

package logbridge

import (
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// InstallGlobal redirects zerolog's package-level global log.Logger
// through a fan-out that writes every event to both passthroughW (the
// operator-facing destination — typically os.Stderr or a file) and
// sink (the runtime.facts capture). Returns a closer that the caller
// must invoke at process exit to drain the Sink synchronously and
// restore the previous global.
//
// Side effect: also swaps zerolog.ErrorMarshalFunc to
// eh.MarshalError so any .Err(boxerErr) call serializes the wrapped
// chain as the structured {streams:[{name:[facts]}]} shape rather
// than a flat string. The Sink decoder recognises this shape and
// projects it into LogRow.ErrorContext (typed *LogErrorContext) for
// the logviewer detail pane's tree renderer, while still populating
// LogRow.Error with a flat summary so table-column / string-only
// consumers don't have to know about the structured form. The
// closer restores the previous marshaler so repeated test setups
// don't leak the global mutation.
//
// Limitation: zerolog.Logger does not expose its underlying writer, so
// any previous formatting configured by boxer's --logFormat flag is
// dropped — the passthrough writer receives the same byte payload the
// Sink decodes (CBOR with the `binary_log` build tag, JSON otherwise).
// Hosts that need a specific operator-facing format must wrap
// passthroughW themselves (e.g. zerolog.ConsoleWriter{Out: os.Stderr})
// and hand the wrapped writer in.
//
// The caller is responsible for the order: InstallGlobal must run AFTER
// any boxer-flag Actions that touch log.Logger (otherwise those will
// overwrite our wrapping); App.Before in urfave/cli is a natural seam.
func InstallGlobal(passthroughW io.Writer, sink *Sink) (closer func() error) {
	prev := log.Logger
	prevErrMarshal := zerolog.ErrorMarshalFunc
	log.Logger = NewLogger(passthroughW, sink)
	zerolog.ErrorMarshalFunc = eh.MarshalError
	closer = func() (err error) {
		if sink != nil {
			err = sink.Close()
		}
		log.Logger = prev
		zerolog.ErrorMarshalFunc = prevErrMarshal
		return
	}
	return
}

// NopCloser returns a closer that does nothing. Useful when a host's
// fact-capture is conditionally disabled and the caller wants a
// uniform deferred Close() call at shutdown.
func NopCloser() (closer func() error) {
	closer = func() (err error) { return }
	return
}

// suppressLogger compile-time-asserts the package's logger reference
// is bound. Tests rely on InstallGlobal to swap log.Logger; this guards
// against a renamed import in a refactor breaking the wire silently.
var _ zerolog.Logger = log.Logger
