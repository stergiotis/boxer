package mdedit

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the per-process AppI descriptor. Static; every newApp() returns
// the same Manifest value.
//
// Three declared capabilities, and the interesting one is the capability that
// is NOT here.
//
// clipboard.write is the document's way out to the clipboard; the way IN is
// egui's own paste, which happens inside the TextEdit and needs no capability
// at all — the asymmetry is the broker's, which serves no read subject
// (ADR-0178 §Context).
//
// fs.dialog.read and fs.dialog.write are the two file gestures (M4), and they
// authorise ASKING, not reaching: each one opens a picker the user must
// approve, and neither names a path. `fs.handle.>` is deliberately absent even
// though a granted handle is addressed under it — the broker adds the narrow
// `fs.handle.{uuid}.>` to this app's live cap set at the moment the user
// approves, and revokes it on close (fsbroker.Service.Resolve). Declaring the
// wildcard statically would trade that for standing authority over every
// handle the broker ever mints, in exchange for nothing: the dynamic grant is
// what the flows actually run on.
//
// PersistedKeys makes the host inject runtime.persist.{ownAlias}.> rather than
// the app repeating the cap boilerplate. It survives the window where a file
// handle does not — handles die with the bus client — so the store stays the
// crash net beside the file rather than being replaced by it.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/mdedit",
	Version:  "0.1.0",
	Display:  "mdedit",
	Title:    "mdedit — markdown editor",
	Icon:     icons.PhMarkdownLogo,
	Topics:   []app.TopicT{app.TopicCode},
	Keywords: []string{"markdown", "editor", "notes", "preview", "obsidian", "writing"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		// Wide enough that the source and the preview both read comfortably
		// at the default split, tall enough for a screenful of either.
		PreferredWidth:  1180,
		PreferredHeight: 820,
	},
	Caps: []app.SubjectFilter{
		{
			Pattern:   clipboardbroker.SubjectWrite,
			Direction: app.CapDirectionPub,
			Reason:    "mdedit: copy the document to the clipboard",
		},
		{
			Pattern:   fsbroker.SubjectDialogRead,
			Direction: app.CapDirectionPub,
			Reason:    "mdedit: Open — raise the file picker to load a document the user chooses",
		},
		{
			Pattern:   fsbroker.SubjectDialogWrite,
			Direction: app.CapDirectionPub,
			Reason:    "mdedit: Save — raise the save picker; later saves reuse the granted handle",
		},
	},
	PersistedKeys: []string{docKey},
}

// init registers the app into app.DefaultRegistry. Factory ctor so two open
// windows get independent App state — including independent buffers, which
// share one persist key and therefore one restored document.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("mdedit: failed to register factory")
	}
}
