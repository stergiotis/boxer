package writingstylescope

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// manifest is the per-process AppI descriptor. Static; every newApp() returns
// the same Manifest value.
//
// The measurement half touches nothing: no bus, no store, no filesystem. The
// three declared caps are the "Open in play" handover and nothing else
// (ADR-0175 §SD7) — publish the cross-matrix as an ephemeral dataset, retract
// it when the window closes, ask the window host for a play window on it.
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
	SurfaceHints: app.SurfaceHints{
		// Sized for the two side-by-side paste panes on the Documents tab and
		// the full-width heatmap plus its colour bar and pair table on the
		// Matrix tab.
		PreferredWidth:  980,
		PreferredHeight: 900,
	},
	Caps: []app.SubjectFilter{
		{
			Pattern:   adhocdata.SubjectPublish,
			Direction: app.CapDirectionPub,
			Reason:    "writingstylescope: publish the section cross-matrix as an ephemeral dataset for the play handover (ADR-0134)",
		},
		{
			Pattern:   adhocdata.SubjectRetract,
			Direction: app.CapDirectionPub,
			Reason:    "writingstylescope: retract that dataset when the window closes",
		},
		{
			Pattern:   windowhost.OpenSubject,
			Direction: app.CapDirectionPub,
			Reason:    "writingstylescope: Open in play — a play window seeded with the query behind the pairs table (ADR-0135 §SD7)",
		},
	},
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
