package writingstylescope

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// helpFS embeds the inline help (apps/writingstylescope/help/*.md), indexed by the
// help library and shown by the HelpHost on F1 (ADR-0047).
//
//go:embed help/*.md
var helpFS embed.FS

// manifest is the per-process AppI descriptor. Static; every newApp() returns
// the same Manifest value.
//
// The measurement touches no filesystem. The caps are the "Open in play"
// handover (ADR-0175 §SD7) — publish the cross-matrix as an ephemeral
// dataset, ask the window host for a play window on it; the retract is the
// runtime's — and the task subjects the sweep and the handover run under.
// The two drafts and the length floor persist under the declared keys
// (ADR-0185), which the host routes without a cap of their own.
var manifest = app.Manifest{
	Id:      "github.com/stergiotis/boxer/apps/writingstylescope",
	Version: "0.1.0",
	Display: "Writing style scope",
	Title:   "Writing style scope — shared writing between two documents",
	Summary: "Measure shared writing between two documents",
	Icon:    icons.PhGitDiff,
	Topics:  []app.TopicT{app.TopicData},
	Keywords: []string{"stylometry", "plagiarism", "compression", "similarity",
		"ncd", "markdown", "documents", "authorship"},
	Surface: app.SurfaceWindowed,
	Help:    help.MustSub(helpFS, "help"),
	SurfaceHints: app.SurfaceHints{
		// Sized for the two side-by-side paste panes on the Documents tab and
		// the full-width heatmap plus its colour bar and pair table on the
		// Matrix tab.
		PreferredWidth:  980,
		PreferredHeight: 900,
	},
	PersistedKeys: []string{keyDocA, keyDocB, keyFloor},
	Caps: append([]app.SubjectFilter{
		{
			Pattern:   adhocdata.SubjectPublish,
			Direction: app.CapDirectionPub,
			Reason:    "writingstylescope: publish the section cross-matrix as an ephemeral dataset for the play handover (ADR-0134)",
		},
		{
			Pattern:   windowhost.OpenSubject,
			Direction: app.CapDirectionPub,
			Reason:    "writingstylescope: Open in play — a play window seeded with the query behind the pairs table (ADR-0135 §SD7)",
		},
	}, task.ProducerCaps()...),
}

// init registers the app into app.DefaultRegistry. Factory ctor so two open
// windows get independent App state — including independent confidence-band
// warm-up jobs.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("writingstylescope: failed to register factory")
	}
}
