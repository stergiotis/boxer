//go:build llm_generated_opus47

package regex_explorer

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// manifest is the static AppI descriptor every instance returns. Kept
// package-level so the factory ctor can hand a fresh instance back
// without re-running Manifest validation.
var manifest = runtimeapp.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer",
	Version:  "0.1.0",
	Display:  "Regex explorer",
	Title:    "Regex Explorer",
	Icon:     "🔍",
	Category: "Tools",
	Surface:  runtimeapp.SurfaceWindowed,
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  styletokens.SurfaceWorkspace.W,
		PreferredHeight: styletokens.SurfaceWorkspace.H,
	},
	Caps: []runtimeapp.SubjectFilter{
		{
			Pattern:   chLocalCapPattern,
			Direction: runtimeapp.CapDirectionPub,
			Reason:    "interactive regex evaluation via clickhouse-local",
			Sticky:    true,
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
