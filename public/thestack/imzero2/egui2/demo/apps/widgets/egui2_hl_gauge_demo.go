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
// the IDS design system: a plain neutral track, the TrafficLight preset with
// a colour-by-value needle, and explicit semantic-tone zones at the large
// size preset. The 270° sweep, painter-drawn arc bands (thick stroked
// polylines — the painter has no native arc), needle, ticks, and centre
// readout are all the gauge widget; every colour / size / stroke is an IDS
// token and zones are semantic styletokens.Tone values carrying labels so
// colour is never the sole encoding channel (ADR-0031 §SD5).
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
			"sampled along the arc; the needle, hub, ticks, and text are lines / a " +
			"circle / PaintText, flushed into an inline canvas with PaintCanvas " +
			"(the treemap / colorscale substrate). Every colour, type size, and " +
			"stroke width is an IDS token; zones are semantic styletokens.Tone " +
			"values, each with a Label so colour is never the sole signal " +
			"(ADR-0031 §SD5). Read-only by design — rotary input is the " +
			"imgui_knobs widget. Demonstrated: a plain neutral track, the " +
			"TrafficLight preset with a colour-by-value needle, and explicit " +
			"Success/Warning/Error tone zones at the large size preset.",
		Render: demoGauge,
	})
}

func demoGauge(ids *c.WidgetIdStack) {
	c.Label("Read-only radial dials — one scalar judged against IDS-toned zones:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())

	for range c.Horizontal().KeepIter() {
		// 1. Plain neutral track — no zones, default (5) major ticks.
		gauge.New("gauge-latency").
			Range(0, 500).
			Suffix(" ms").
			Label("Latency").
			Render(ids.PrepareSeq(0x6A0301), 240)
		c.AddSpace(gapSections())

		// 2. TrafficLight preset + colour-by-value needle.
		gauge.New("gauge-cpu").
			Range(0, 100).
			Zones(gauge.TrafficLight(0, 100)...).
			NeedleFollowsZone(true).
			Suffix("%").
			Label("CPU").
			Render(ids.PrepareSeq(0x6A0302), 78)
		c.AddSpace(gapSections())

		// 3. Explicit semantic tone zones at the large preset, with minor ticks.
		gauge.New("gauge-temp").
			Range(0, 120).
			Size(gauge.SizeLg).
			Ticks(7, 1).
			Zones(
				gauge.Zone{From: 0, To: 60, Tone: styletokens.ToneSuccess, Label: "ok"},
				gauge.Zone{From: 60, To: 90, Tone: styletokens.ToneWarning, Label: "warm"},
				gauge.Zone{From: 90, To: 120, Tone: styletokens.ToneError, Label: "hot"},
			).
			NeedleFollowsZone(true).
			Suffix("°C").
			Label("Temp").
			Render(ids.PrepareSeq(0x6A0303), 88)
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
