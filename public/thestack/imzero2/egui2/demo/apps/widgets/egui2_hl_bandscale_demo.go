package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bandscale"
)

// =============================================================================
// bandscale widget demo — a linear scale of named bands with markers (ADR-0280)
//
// One ladder drawn three ways: with a limit and a current position, with one
// marker, and compact for a toolbar row. A slider moves the current position,
// so the marker can be watched crossing bands.
// =============================================================================

func init() {
	registry.Register(registry.Demo{
		Name:     "bandscale",
		Category: "Charts & plots",
		Title:    icons.IconChartLine + " bandscale",
		Stage:    [2]float32{800, 420},
		Kind:     registry.DemoKindUX,
		Description: "A linear scale of equal, named bands with markers on it: an " +
			"ordinal ladder where a position is judged against the band it falls " +
			"in. Each band carries a label and a semantic tone and each marker a " +
			"label, so colour is never the only channel (ADR-0031 §SD5); a hollow " +
			"marker reads as the limit of two. Painted into one inline canvas. " +
			"Demonstrated: a limit and a current position on one scale, a single " +
			"marker, and the compact form for a toolbar row.",
		Render: demoBandscale,
	})
}

// bandscaleDemoNow is the demo's current position, bound to its slider.
var bandscaleDemoNow = 0.42

var bandscaleDemoLadder = []bandscale.Band{
	{Label: "talk", Tone: styletokens.ToneNeutral},
	{Label: "read", Tone: styletokens.ToneInfo},
	{Label: "view", Tone: styletokens.ToneSuccess},
	{Label: "edit", Tone: styletokens.ToneWarning, Shade: bandscale.ShadeStrong},
	{Label: "run", Tone: styletokens.ToneWarning},
	{Label: "outside", Tone: styletokens.ToneError},
}

func demoBandscale(ids *c.WidgetIdStack) {
	c.Label("A ladder of named bands; the markers say where a limit and a current position sit on it:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())
	c.SliderF64(ids.PrepareStr("bandscale-now"), bandscaleDemoNow, 0, 1).Text("now").SendRespVal(&bandscaleDemoNow)
	c.AddSpace(padInner())

	bandscale.Render(bandscale.Input{Ids: ids, ScopeKey: "bandscale-two", Bands: bandscaleDemoLadder, Width: 520,
		Markers: []bandscale.Marker{{Position: 0.78, Label: "may: run", Hollow: true}, {Position: bandscaleDemoNow, Label: "now"}}})
	c.AddSpace(gapSections())
	bandscale.Render(bandscale.Input{Ids: ids, ScopeKey: "bandscale-one", Bands: bandscaleDemoLadder[:4], Width: 320,
		Markers: []bandscale.Marker{{Position: bandscaleDemoNow, Label: "now"}}})
	c.AddSpace(gapSections())
	for range c.HorizontalTop().KeepIter() {
		bandscale.Render(bandscale.Input{Ids: ids, ScopeKey: "bandscale-compact", Bands: bandscaleDemoLadder, Width: 140, Compact: true,
			Markers: []bandscale.Marker{{Position: 0.78, Hollow: true}, {Position: bandscaleDemoNow}}})
		c.Label("Compact, with the position named beside it").Selectable(false).Send()
	}
}
