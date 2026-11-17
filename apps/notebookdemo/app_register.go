// Package notebookdemo is a host that embeds a notebook's live reading in a
// region of its own frame (ADR-0380 §SD5, M4): the first embedder of
// package notebook's pane. It draws content of its own above the pane —
// which notebook is shown, and what the host reads of the pane's state —
// and embeds two notebooks, each the way the ADR names: a registered one by
// its slug (notebooklet.Embed), and a document of the host's own that no
// launcher lists (notebook.NewEmbedded).
//
// The host keeps the governance: the panes' capabilities are on its
// manifest (notebook.EmbedCaps), it drives their Mount, Frame and Unmount,
// and their runs are attributed to `<this app>#<key>`.
package notebookdemo

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// helpFS embeds the help book (apps/notebookdemo/help/*.md).
//
//go:embed help/*.md
var helpFS embed.FS

// ManifestId is this app's identity — its Go import path (ADR-0026 id rule).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/notebookdemo"

var manifest = app.Manifest{
	Id:           ManifestId,
	Version:      "0.1.0",
	Display:      "Embedded notebook demo",
	Title:        "Embedded notebook demo",
	Summary:      "A host that draws a notebook's live reading in a region of its own window",
	Icon:         icons.PhNotebook,
	Topics:       []app.TopicT{app.TopicData},
	Keywords:     []string{"notebook", "embed", "live", "pane", "adr-0380"},
	Kind:         app.KindDemo,
	Surface:      app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{PreferredWidth: 1100, PreferredHeight: 900},
	// What the panes need, and nothing of collection or the store: a
	// document widens none of it (ADR-0380 §SD5).
	Caps:       notebook.EmbedCaps(),
	Operations: ops.Catalog(),
	Help:       help.MustSub(helpFS, "help"),
}

func init() {
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("notebookdemo: failed to register factory")
	}
}
