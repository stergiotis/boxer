package mdedit

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the per-process AppI descriptor. Static; every newApp() returns
// the same Manifest value.
//
// One declared capability. clipboard.write is the document's only way out; the
// way IN is egui's own paste, which happens inside the TextEdit and needs no
// capability at all — the asymmetry is the broker's, which serves no read
// subject (ADR-0178 §Context). There is deliberately no fs.* cap: this cut
// does not touch the filesystem.
//
// PersistedKeys makes the host inject runtime.persist.{ownAlias}.> rather than
// the app repeating the cap boilerplate. It is what keeps a no-file-I/O editor
// from losing its document when the window closes.
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
			Reason:    "mdedit: copy the document to the clipboard — the only way text leaves the app",
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
