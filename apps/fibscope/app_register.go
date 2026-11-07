package fibscope

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/fibscope/launchcfg"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// helpFS embeds the inline help (apps/fibscope/help/*.md), indexed by the
// help library and shown by the HelpHost on F1 (ADR-0047).
//
//go:embed help/*.md
var helpFS embed.FS

// manifest is the per-process AppI descriptor. Static; every newApp() returns
// the same Manifest value.
//
// No Caps are declared: the app is a pure front-end over the identity packages
// — it mints nothing and touches neither the bus nor the persist store.
// LaunchKind declares the config another app opens a window with (launchcfg,
// ADR-0135); Operations is the catalog of fibscope_ops.go.
var manifest = app.Manifest{
	Id:         launchcfg.AppId,
	Version:    "0.1.0",
	Display:    "Fibscope",
	Title:      "Fibscope — fibonacci-tagged id explorer",
	Summary:    "Explore fibonacci-tagged identifier sequences",
	Icon:       icons.PhBinary,
	Topics:     []app.TopicT{app.TopicData},
	Keywords:   []string{"fibonacci", "sequence", "numbers"},
	Surface:    app.SurfaceWindowed,
	Help:       help.MustSub(helpFS, "help"),
	LaunchKind: launchcfg.Kind,
	Operations: ops.Catalog(),
	SurfaceHints: app.SurfaceHints{
		// Sized to fit the 64-bit strip and the split/SQL readout on the
		// Explore tab, and the trade-off plot above the stats table.
		PreferredWidth:  980,
		PreferredHeight: 860,
	},
}

// idSeedName is the seed variable's name, quoted where it is refused.
const idSeedName = "BOXER_FIBSCOPE_ID"

// IdSeed opens a new window on a word of the caller's choosing (ADR-0009
// seed variable, the BOXER_MDEDIT_DOC shape), so a scene reaches a split
// other than the worked example without typing. A caller's launch config
// wins over it.
var IdSeed = env.NewString(env.Spec{
	Name:        idSeedName,
	Description: "the 64-bit word a new fibscope window opens on, decimal or 0x-prefixed hexadecimal; a word that is not a tagged id is accepted; for scenes and demos",
	Category:    env.CategoryE("boxer-fibscope"),
})

// init registers the app into app.DefaultRegistry. Factory ctor so two open
// windows get independent App state.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("fibscope: failed to register factory")
	}
}
