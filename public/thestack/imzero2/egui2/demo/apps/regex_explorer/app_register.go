//go:build llm_generated_opus47

package regex_explorer

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
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
		PreferredWidth:  1100,
		PreferredHeight: 720,
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
	// Interactive mode registers per-instance *AppInstance values so
	// each open window has its own *App state (pattern, haystack,
	// mode flags, query results, …). Tour mode uses SeededFuncApp —
	// tours are single-instance and read/write the package-level
	// `app` directly via their scene Setup hooks.
	var ctor runtimeapp.AppCtor
	if imzero2env.ScreenshotDir.Get() != "" {
		ctor = func() (a runtimeapp.AppI, err error) {
			a, err = runtimeapp.NewSeededFuncApp(manifest, RenderLoopHandlerTour)
			return
		}
	} else {
		ctor = func() (a runtimeapp.AppI, err error) {
			a = newInstance()
			return
		}
	}
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, ctor)
	if err != nil {
		log.Warn().Err(err).Msg("regex_explorer: failed to register factory")
	}
}
