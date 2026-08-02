package widgets

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// =============================================================================
// ecdf widget demo — empirical CDF with simultaneous BJ / DKW / EP / HC band
//
// Three synthetic distributions pre-sorted at init. The demo renders
// one ECDF + simultaneous confidence band at a time, with live combos
// for the band method (BJ/DKW/EP/HC), the confidence level (0.01 /
// 0.05 / 0.10), and the sample to render. Demonstrates the
// ecdfbands library and the PlotPolygon FFFI2 primitive in one
// composed widget.
// =============================================================================

type ecdfDemoState struct {
	sampleIdx int
	methodIdx int
	alphaIdx  int
	// resetRequested is a one-frame latch set by the "Reset zoom" button and
	// consumed in the plot block (forwarded to PlotFluid.ResetBounds), so the
	// programmatic auto-fit fires exactly on the frame the button was clicked.
	resetRequested bool
}

type ecdfSample struct {
	name   string
	sorted []float64
}

var (
	ecdfDemoSamples = buildEcdfDemoSamples()

	ecdfDemoMethods = []struct {
		label  string
		method ecdfbands.BandMethodE
	}{
		{"Berk-Jones", ecdfbands.BandMethodBerkJones},
		{"DKW-Massart", ecdfbands.BandMethodDKW},
		{"Equal-Precision", ecdfbands.BandMethodEqualPrecision},
		{"Higher-Criticism", ecdfbands.BandMethodHigherCriticism},
	}

	ecdfDemoAlphas = []float64{0.01, 0.05, 0.10}

	// F(x) is bounded to [0, 1], so the y-axis reads best with fixed
	// quarter-point ticks rather than egui_plot's data-driven spacer.
	ecdfDemoYTickVals   = []float64{0, 0.25, 0.5, 0.75, 1}
	ecdfDemoYTickLabels = []string{"0", "0.25", "0.5", "0.75", "1"}
)

func init() {
	registry.Register(registry.Demo{
		Name:     "ecdf",
		Category: "Charts & plots",
		Title:    icons.IconChartLine + " ecdf",
		Stage:    [2]float32{960, 640},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "Empirical CDF with a finite-sample exact simultaneous " +
			"confidence band. Four peer-reviewed band families selectable " +
			"(Berk-Jones, DKW-Massart, equal-precision Stepanova-Wang, " +
			"higher-criticism Donoho-Jin) via the boxer/public/" +
			"analytics/stats/ecdfbands library; band is rendered as " +
			"a PlotPolygon (the FFFI2 primitive added for this widget).",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &ecdfDemoState{
				sampleIdx: 0,
				methodIdx: 0,
				alphaIdx:  1, // α = 0.05
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoEcdf(ids, state.(*ecdfDemoState))
		},
		SourceFunc: demoEcdf,
	})
}

func demoEcdf(ids *c.WidgetIdStack, st *ecdfDemoState) {
	// --- Control row -----------------------------------------------------
	for range c.Horizontal().KeepIter() {
		// Sample selector combo.
		c.Label("Sample:").Send()
		c.AddSpace(padInner())
		curSample := ecdfDemoSamples[st.sampleIdx].name
		for range c.ComboBox(ids.PrepareStr("ecdf-sample-cb"),
			c.WidgetText().Text("sample").Keep(),
			c.WidgetText().Text(curSample).Keep()).KeepIter() {
			for i, s := range ecdfDemoSamples {
				selected := i == st.sampleIdx
				if c.Button(ids.PrepareSeq(uint64(0xECD000+i)),
					c.Atoms().Text(s.name).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.sampleIdx = i
				}
			}
		}
		c.AddSpace(gapSections())

		// Band method combo.
		c.Label("Method:").Send()
		c.AddSpace(padInner())
		curMethod := ecdfDemoMethods[st.methodIdx].label
		for range c.ComboBox(ids.PrepareStr("ecdf-method-cb"),
			c.WidgetText().Text("band").Keep(),
			c.WidgetText().Text(curMethod).Keep()).KeepIter() {
			for i, m := range ecdfDemoMethods {
				selected := i == st.methodIdx
				if c.Button(ids.PrepareSeq(uint64(0xECD100+i)),
					c.Atoms().Text(m.label).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.methodIdx = i
				}
			}
		}
		c.AddSpace(gapSections())

		// Alpha combo.
		c.Label("Alpha:").Send()
		c.AddSpace(padInner())
		curAlpha := fmt.Sprintf("%.2f", ecdfDemoAlphas[st.alphaIdx])
		for range c.ComboBox(ids.PrepareStr("ecdf-alpha-cb"),
			c.WidgetText().Text("level").Keep(),
			c.WidgetText().Text(curAlpha).Keep()).KeepIter() {
			for i, a := range ecdfDemoAlphas {
				selected := i == st.alphaIdx
				label := fmt.Sprintf("%.2f", a)
				if c.Button(ids.PrepareSeq(uint64(0xECD200+i)),
					c.Atoms().Text(label).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.alphaIdx = i
				}
			}
		}
	}
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())

	// Reset-zoom affordance + interaction hint, placed directly above the plot
	// so the controls read as belonging to it. The button latches
	// st.resetRequested; the plot block below consumes it via ResetBounds so the
	// auto-fit fires on the same frame the button is clicked.
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("ecdf-reset-zoom"),
			c.Atoms().Text("Reset zoom").Keep()).
			Small().
			SendResp().HasPrimaryClicked() {
			st.resetRequested = true
		}
		c.AddSpace(gapSections())
		c.LabelAtoms(c.Atoms().BeginRichText(
			"Double-click the plot or use Reset zoom to fit the view").
			Small().Weak().End().Keep()).Send()
	}
	c.AddSpace(padInner())

	// --- Plot ------------------------------------------------------------
	method := ecdfDemoMethods[st.methodIdx].method
	alpha := ecdfDemoAlphas[st.alphaIdx]
	sample := ecdfDemoSamples[st.sampleIdx]

	r := ecdf.New().
		Method(method).
		Alpha(alpha).
		SeriesName(sample.name)

	// The plot renders through the implot port (ADR-0149 SD7). The
	// viewport constraints pin the outer view so zooming out cannot
	// strand the reader in empty space — ECDF support is naturally
	// [0, 1] on Y, and the sample's [min, max] bounds X; zooming in
	// remains unrestricted. The fixed quarter-point marks stay on y;
	// implot's own nice-number locator serves x. FitNext (wired to the
	// Reset zoom button above) re-fits the view, as does a double-click.
	xLo, xHi := sample.sorted[0], sample.sorted[len(sample.sorted)-1]
	// Consume the one-frame reset latch set by the Reset zoom button.
	resetZoom := st.resetRequested
	st.resetRequested = false
	p := implot.Begin(ids, "##ecdf-plot", 900, 500)
	p.SetupAxes("value", "F(x)", implot.AxisFlagsNone, implot.AxisFlagsNone)
	p.SetupAxisTicks(implot.AxisY1, ecdfDemoYTickVals, ecdfDemoYTickLabels)
	p.SetupAxisLimitsConstraints(implot.AxisX1, xLo, xHi)
	p.SetupAxisLimitsConstraints(implot.AxisY1, 0, 1)
	p.IncludeY(0)
	p.IncludeY(1)
	p.NoLegend()
	if resetZoom {
		p.FitNext()
	}
	ch := r.At(p, sample.sorted)
	_ = r.Render(p, sample.sorted)
	r.PaintCrosshair(p, ch)
	p.End()

	c.AddSpace(padInner())
	ecdf.WriteStatusLine(ch)
	c.LabelAtoms(
		c.Atoms().BeginRichText(fmt.Sprintf(
			"n = %d, method = %s, simultaneous (1-α) = %.2f. "+
				"Polygon = confidence band; line = ECDF.",
			len(sample.sorted), ecdfDemoMethods[st.methodIdx].label, 1-alpha)).Small().Weak().End().Keep(),
	).Send()
}

// buildEcdfDemoSamples seeds three distinct sample shapes at moderate
// n. The size is chosen so the Moscovich-Nadler inversion runs in a
// few ms per (method, α) cell — the demo's combo toggles re-invert on
// every change, so we want it snappy.
func buildEcdfDemoSamples() []*ecdfSample {
	type variant struct {
		name string
		gen  func(rnd *rand.Rand) float64
		seed int64
		n    int
	}
	variants := []variant{
		{"N(0, 1) n=80", func(rnd *rand.Rand) float64 { return rnd.NormFloat64() }, 11, 80},
		{"Uniform n=80", func(rnd *rand.Rand) float64 { return rnd.Float64() }, 22, 80},
		{"Exp(λ=1) n=80", func(rnd *rand.Rand) float64 { return rnd.ExpFloat64() }, 33, 80},
	}
	out := make([]*ecdfSample, 0, len(variants))
	for _, v := range variants {
		rnd := rand.New(rand.NewSource(v.seed))
		data := make([]float64, v.n)
		for i := range data {
			data[i] = v.gen(rnd)
		}
		sort.Float64s(data)
		out = append(out, &ecdfSample{name: v.name, sorted: data})
	}
	return out
}
