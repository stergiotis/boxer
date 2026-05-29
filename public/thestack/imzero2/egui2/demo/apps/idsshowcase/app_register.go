//go:build llm_generated_opus47

package idsshowcase

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

var manifest = runtimeapp.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/idsshowcase",
	Version:  "0.1.0",
	Display:  "IDS showcase",
	Title:    "IDS token catalogue",
	Icon:     "🎨",
	Category: "Demos",
	Surface:  runtimeapp.SurfaceWindowed,
	SurfaceHints: runtimeapp.SurfaceHints{
		PreferredWidth:  860,
		PreferredHeight: 620,
	},
}

// RenderTourPalette renders the neutral-spine + semantic-palette sections
// of the IDS showcase as a screenshot-tour capture. The Rust screenshot
// pipeline caps PNG height at ~694 px, so the original single-image
// showcase couldn't fit all 8 sections — see ADR-0037 follow-on
// discussion. Splitting into 4 section PNGs is the workaround.
func RenderTourPalette(ids *c.WidgetIdStack) {
	inst := &App{ids: ids, density: styletokens.DensityFromEnv()}
	c.Label("IDS palette — ADR-0031 §SD2 + §SD4").Send()
	c.Label(fmt.Sprintf("active density: %s   (IMZERO2_DENSITY)", inst.density.String())).Send()
	c.Separator().Horizontal().Send()
	inst.renderNeutralSpine()
	c.AddSpace(styletokens.GapSections(inst.density))
	c.Label("Semantic palette — 6 roles × 3 emphasis (ADR-0031 §SD2)").Send()
	inst.renderSemanticPalette()
}

// RenderTourTypography renders the type-scale section.
func RenderTourTypography(ids *c.WidgetIdStack) {
	inst := &App{ids: ids, density: styletokens.DensityFromEnv()}
	c.Label("IDS typography — ADR-0030 §SD3").Send()
	c.Label(fmt.Sprintf("active density: %s   (IMZERO2_DENSITY)", inst.density.String())).Send()
	c.Separator().Horizontal().Send()
	c.Label("Type scale — IDS-bound TextStyle slots").Send()
	inst.renderTypeScale()
}

// RenderTourEncoding renders the data-encoding palettes + the egui_plot
// integration sample (QualitativeCycle-driven series colors).
func RenderTourEncoding(ids *c.WidgetIdStack) {
	inst := &App{ids: ids, density: styletokens.DensityFromEnv()}
	c.Label("IDS data encoding — ADR-0031 §SD3 (Crameri / viridis)").Send()
	c.Label(fmt.Sprintf("active density: %s   (IMZERO2_DENSITY)", inst.density.String())).Send()
	c.Separator().Horizontal().Send()
	c.Label("Data encoding — scientifically-published colormaps").Send()
	inst.renderDataEncoding()
	c.AddSpace(styletokens.GapSections(inst.density))
	c.Label("Data encoding in egui_plot — QualitativeCycle drives series colors").Send()
	inst.renderDataEncodingPlot()
}

// RenderTourGeometry renders the density spec + rounding ladder + stroke
// ladder — the three non-color foundation surfaces.
func RenderTourGeometry(ids *c.WidgetIdStack) {
	inst := &App{ids: ids, density: styletokens.DensityFromEnv()}
	c.Label("IDS geometry — ADR-0032 (density / rounding / stroke)").Send()
	c.Label(fmt.Sprintf("active density: %s   (IMZERO2_DENSITY)", inst.density.String())).Send()
	c.Separator().Horizontal().Send()
	inst.renderDensitySpec()
	c.AddSpace(styletokens.GapSections(inst.density))
	c.Label("Rounding ladder — density-independent (ADR-0032 §SD3)").Send()
	inst.renderRoundingLadder()
	c.AddSpace(styletokens.GapSections(inst.density))
	c.Label("Stroke ladder — density-independent (ADR-0032 §SD4)").Send()
	inst.renderStrokeLadder()
}

func init() {
	err := runtimeapp.DefaultRegistry.RegisterFactory(manifest, func() (a runtimeapp.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("idsshowcase: failed to register factory")
	}
	// Demo-registry hooks for the screenshot tour. The carousel resolve
	// blank-imports this package so this init runs whenever the carousel
	// is loaded — the tour driver picks the demos up automatically.
	//
	// The showcase is split across four entries because the Rust
	// screenshot pipeline caps PNG height at ~694 px, below what the full
	// 8-section showcase needs (~1400 px). Each section captures as its
	// own well-sized PNG.
	registry.Register(registry.Demo{
		Name:        "idsshowcase-palette",
		Category:    "Design system",
		Title:       icons.IconPalette + " IDS palette",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "IDS palette catalogue — neutral spine + semantic palette (6 roles × 3 emphasis).",
		Render:      RenderTourPalette,
		SourceFunc:  (*App)(nil).renderSemanticPalette,
	})
	registry.Register(registry.Demo{
		Name:        "idsshowcase-typography",
		Category:    "Design system",
		Title:       icons.IconPalette + " IDS typography",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "IDS type scale — Display / Heading / Body / Caption / Micro / Body.Mono.",
		Render:      RenderTourTypography,
		SourceFunc:  (*App)(nil).renderTypeScale,
	})
	registry.Register(registry.Demo{
		Name:        "idsshowcase-encoding",
		Category:    "Design system",
		Title:       icons.IconPalette + " IDS data encoding",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "IDS data encoding — Crameri qualitative / sequential / diverging strips + egui_plot QualitativeCycle integration.",
		Render:      RenderTourEncoding,
		SourceFunc:  (*App)(nil).renderDataEncoding,
	})
	registry.Register(registry.Demo{
		Name:        "idsshowcase-geometry",
		Category:    "Design system",
		Title:       icons.IconPalette + " IDS geometry",
		Stage:       [2]float32{1024, 700},
		Kind:        registry.DemoKindUX,
		Description: "IDS geometry — density-resolved spacing spec + rounding ladder + stroke ladder.",
		Render:      RenderTourGeometry,
		SourceFunc:  (*App)(nil).renderRoundingLadder,
	})
}
