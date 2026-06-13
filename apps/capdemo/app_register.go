package capdemo

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the per-process AppI descriptor. Static; every newApp()
// returns the same Manifest value. The Caps declarations are the
// point of the demo: a real-world app spelling out exactly what it
// needs from the runtime.
//
//   - fs.dialog.read — request the file picker. Pub only; the broker
//     subscribes to fs.> and replies on the request inbox.
//   - fs.dialog.watch — request the folder-watch picker. Pub only;
//     the broker mints a HandleModeWatch handle on Resolve.
//   - fs.handle.> — publish read/watch/unwatch/close requests on any
//     granted handle. Declared eagerly; the broker also augments the
//     client's caps with the narrower fs.handle.{uuid}.> on Resolve
//     (CapDirectionBoth for watch handles, so .event subscribe is
//     allowed). Future commits can narrow this manifest cap once a
//     "request-specific narrow grant" path is preferred.
//   - PersistedKeys → host auto-injects runtime.persist.{ownAlias}.>
//     so the app doesn't repeat the boilerplate cap pattern. The
//     scratchpad key is the single value this demo persists.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/capdemo",
	Version:  "0.1.0",
	Display:  "Capability broker",
	Title:    "Capability broker",
	Icon:     icons.PhLockKey,
	Category: "Demos",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  720,
		PreferredHeight: 480,
	},
	Caps: []app.SubjectFilter{
		{
			Pattern:   fsbroker.SubjectDialogRead,
			Direction: app.CapDirectionPub,
			Reason:    "demo: request a user-picked file via Powerbox",
		},
		{
			Pattern:   fsbroker.SubjectDialogWatch,
			Direction: app.CapDirectionPub,
			Reason:    "demo: request a folder-watch dialog via Powerbox",
		},
		{
			Pattern:   fsbroker.HandleSubjectPrefix + ">",
			Direction: app.CapDirectionPub,
			Reason:    "demo: publish read/watch/unwatch/close on the granted handle",
		},
		{
			Pattern:   clipboardbroker.SubjectWrite,
			Direction: app.CapDirectionPub,
			Reason:    "demo: copy code blocks to the clipboard via the markdown copy button",
		},
	},
	PersistedKeys: []string{scratchpadKey},
}

// init registers the demo into app.DefaultRegistry. Factory ctor so
// two open windows get independent App state. No screenshot tour
// mode — the goroutine-driven async picker doesn't compose with the
// 4-frame tour, and the demo's purpose is interactive validation.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("capdemo: failed to register factory")
	}
}
