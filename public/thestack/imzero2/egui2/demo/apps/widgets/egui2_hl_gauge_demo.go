package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/gauge"
)

// =============================================================================
// gauge widget demo — read-only radial dials (ADR-0068)
//
// Three dials in a row exercise the gauge's range of configuration against
// the IDS design system: a plain neutral track, the TrafficLight preset, and
// explicit semantic-tone zones at the large size preset. The 270° sweep,
// painter-drawn arc bands (thick stroked polylines — the painter has no native
// arc), rhomboid needle, ticks, and centre readout are all the gauge widget;
// every colour / size / stroke is an IDS token and zones are semantic
// styletokens.Tone values carrying labels so colour is never the sole encoding
// channel (ADR-0031 §SD5). The needle is a neutral monochrome shape — it
// encodes the value by angle, not colour.
// =============================================================================

func init() {
	registry.Register(registry.Demo{
		Name:     "gauge",
		Category: "Charts & plots",
		Title:    icons.IconChartLine + " gauge",
		Stage:    [2]float32{800, 560},
		Kind:     registry.DemoKindUX,
		Description: "Read-only radial dial: one scalar mapped onto a bounded " +
			"[min,max] range and drawn as a ~270° needle dial with optional " +
			"colored zones, ticks, and a centre value readout. The painter has no " +
			"native arc primitive, so each zone band is a thick stroked polyline " +
			"sampled along the arc; the needle is a filled rhomboid polygon; the hub, ticks, and text are lines, a" +
			"circle / PaintText, flushed into an inline canvas with PaintCanvas " +
			"(the treemap / colorscale substrate). Every colour, type size, and " +
			"stroke width is an IDS token; zones are semantic styletokens.Tone " +
			"values, each with a Label so colour is never the sole signal " +
			"(ADR-0031 §SD5). Read-only by design — rotary input is the " +
			"imgui_knobs widget. Demonstrated: a plain neutral track, the " +
			"TrafficLight preset, and explicit " +
			"Success/Warning/Error tone zones at the large size preset. The needle is a neutral monochrome shape — the value is read from its angle, not its colour.",
		Render: demoGauge,
	})
}

// gaugeDemoFit keeps each dial's readout-fit memo across frames.
var gaugeDemoFit [3]gauge.State

func demoGauge(ids *c.WidgetIdStack) {
	c.Label("Read-only radial dials — one scalar judged against IDS-toned zones:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())

	for range c.Horizontal().KeepIter() {
		// 1. Plain neutral track — no zones, default (5) major ticks.
		gauge.Render(gauge.Input{
			Ids: ids, ScopeKey: "gauge-latency", Value: 240,
			Min: 0, Max: 500, Suffix: " ms", Label: "Latency",
			State: &gaugeDemoFit[0],
		})
		c.AddSpace(gapSections())

		// 2. TrafficLight preset + colour-by-value needle.
		gauge.Render(gauge.Input{
			Ids: ids, ScopeKey: "gauge-cpu", Value: 78,
			Min: 0, Max: 100, Zones: gauge.TrafficLight(0, 100), Suffix: "%", Label: "CPU",
			State: &gaugeDemoFit[1],
		})
		c.AddSpace(gapSections())

		// 3. Explicit semantic tone zones at the large preset, with minor ticks.
		gauge.Render(gauge.Input{
			Ids: ids, ScopeKey: "gauge-temp", Value: 88,
			Min: 0, Max: 120, Size: gauge.SizeLg, MajorTicks: 7, MinorTicks: 1,
			Zones: []gauge.Zone{
				{From: 0, To: 60, Tone: styletokens.ToneSuccess, Label: "ok"},
				{From: 60, To: 90, Tone: styletokens.ToneWarning, Label: "warm"},
				{From: 90, To: 120, Tone: styletokens.ToneError, Label: "hot"},
			},
			Suffix: "°C", Label: "Temp",
			State: &gaugeDemoFit[2],
		})
	}

	c.AddSpace(padInner())
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())
	c.LabelAtoms(
		c.Atoms().BeginRichText("All three dials are read-only; the 270° sweep, " +
			"painter-drawn arc bands, needle, ticks, and centre readout are the " +
			"gauge widget (ADR-0068). Zones are semantic styletokens.Tone with " +
			"labels, so the dials stay legible in monochrome / high-contrast modes.").
			Small().Weak().End().Keep(),
	).Send()
}
