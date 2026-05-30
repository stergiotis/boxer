//go:build llm_generated_opus47

package helphost

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// helpFS embeds the HelpHost's own help corpus — "Help about Help".
// The manifest exposes a fs.Sub-rooted view so doc paths in the
// indexed book are `overview`, `howto/add-help` rather than carrying
// the `help/` prefix.
//
//go:embed help
var helpFS embed.FS

// manifest is the static AppI descriptor every HelpHost instance
// returns. Kept package-level so the factory ctor can hand a fresh
// instance back without re-running Manifest validation.
//
// Phosphor `book-open` is the icon: a book that's actively being
// read (mirrors what a user sees the moment they invoke the app),
// distinct from PhBook (a closed book — a corpus, not an act of
// reading).
var manifest = app.Manifest{
	Id:       ManifestId,
	Version:  "0.1.0",
	Display:  "Help center",
	Title:    "Help center",
	Icon:     icons.PhBookOpen,
	Category: "Runtime",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  900,
		PreferredHeight: 640,
	},
	// The reader's one capability: copy a rendered code/verbatim block to
	// the clipboard via the per-block copy button (ADR-0026 Update
	// 2026-05-30). Pub only — the broker subscribes to clipboard.write and
	// the windowed host drains the queued text into an egui copy_text op.
	Caps: []app.SubjectFilter{
		{
			Pattern:   clipboardbroker.SubjectWrite,
			Direction: app.CapDirectionPub,
			Reason:    "copy help code blocks to the clipboard",
		},
	},
	// Self-documenting corpus: an overview of the Help app and a
	// how-to for wiring help docs into a new app's manifest. Anyone
	// opening Help for the first time can find their way around from
	// here, and an app author looking for the embed pattern doesn't
	// need to leave the running binary.
	Help: help.MustSub(helpFS, "help"),
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = New()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("helphost: failed to register factory")
	}
}
