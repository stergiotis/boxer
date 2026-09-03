package regex_explorer

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// manifest is the static AppI descriptor every instance returns. Kept
// package-level so the factory ctor can hand a fresh instance back
// without re-running Manifest validation.
var manifest = runtimeapp.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer",
	Version:  "0.1.0",
	Display:  "Regex explorer",
	Title:    "Regex Explorer",
	Summary:  "Test a regular expression against sample text live",
	Icon:     icons.PhMagnifyingGlass,
	Topics:   []runtimeapp.TopicT{runtimeapp.TopicCode},
	Keywords: []string{"regex", "regexp", "pattern", "match", "text"},
	Surface:  runtimeapp.SurfaceWindowed,
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  styletokens.SurfaceWorkspace.W,
		PreferredHeight: styletokens.SurfaceWorkspace.H,
	},
	Caps: []runtimeapp.SubjectFilter{
		{
			Pattern:   ChLocalCapPattern,
			Direction: runtimeapp.CapDirectionPub,
			Reason:    "interactive regex evaluation via clickhouse-local",
			Sticky:    true,
		},
		// The ADR-0017 extraction hand-off. Without these three the
		// button is still drawn, and each publish is refused with a
		// reason in the status line — which is the honest degradation,
		// but only these caps make it work.
		{
			Pattern:   adhocdata.SubjectPublish,
			Direction: runtimeapp.CapDirectionPub,
			Reason:    "regex_explorer: publish the Go and ClickHouse extraction as ad-hoc datasets (ADR-0134)",
		},
		{
			Pattern:   adhocdata.SubjectRetract,
			Direction: runtimeapp.CapDirectionPub,
			Reason:    "regex_explorer: retract those datasets when the window closes",
		},
		{
			Pattern:   windowhost.OpenSubject,
			Direction: runtimeapp.CapDirectionPub,
			Reason:    "regex_explorer: open a play window joined over both engines' extraction (ADR-0135 §SD7)",
		},
	},
}

func init() {
	// regex_explorer registers per-instance *AppInstance values so each
	// open window has its own *App state (pattern, haystack, mode flags,
	// query results, …). Screenshot capture is handled centrally by the
	// widgets TestDriver via the Demos registered in regex_explorer_tour.go
	// (ADR-0057).
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, func() (a runtimeapp.AppI, err error) {
		a = newInstance()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("regex_explorer: failed to register factory")
	}
}
