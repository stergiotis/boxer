package app

import (
	"github.com/rs/zerolog"
)

// AppLogger returns a zerolog.Logger that pre-tags every event with the
// app_id context field. When the host has installed a logbridge.Sink on
// the base logger's writer chain (via logbridge.NewLogger), the field
// becomes the per-row MembRuntimeApp tag on the boxer.facts row —
// readers can filter logs by app without the host wiring a separate
// Sink per app. Hosts that haven't installed a Sink still benefit: the
// field shows up in stdout / file logs for ordinary debugging.
//
// The returned logger is an independent context — modifying it (further
// .With() additions) does not affect base.
func AppLogger(base zerolog.Logger, appId AppIdT) (logger zerolog.Logger) {
	logger = base.With().Str("app_id", string(appId)).Logger()
	return
}
