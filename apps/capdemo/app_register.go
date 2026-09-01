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
//   - fs.handle.> — deliberately NOT declared, which is as much the point
//     of the demo as the two above. It used to be, "eagerly", with a note
//     that a request-specific narrow grant would be preferable; that is
//     what the broker was already doing. Resolve adds the narrow
//     fs.handle.{uuid}.> to this client the moment the USER approves a
//     dialog and revokes it on close (CapDirectionBoth for watch handles,
//     so the .event subscribe is allowed — a half this manifest never
//     carried anyway, since its entry was Pub only). Declaring the
//     wildcard therefore bought nothing and cost the distinction between
//     a per-file, revocable, user-approved authority and a standing one
//     over every handle the broker ever mints. The round-trip tests run
//     on these caps alone, watch included, which is what shows it.
//   - PersistedKeys → host auto-injects runtime.persist.{ownAlias}.>
//     so the app doesn't repeat the boilerplate cap pattern. The
//     scratchpad key is the single value this demo persists.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/capdemo",
	Version:  "0.1.0",
	Display:  "Capability broker",
	Title:    "Capability broker",
	Summary:  "Request filesystem and clipboard grants through the broker",
	Icon:     icons.PhLockKey,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"capability", "broker", "permission", "grant"},
	Kind:     app.KindDemo,
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
