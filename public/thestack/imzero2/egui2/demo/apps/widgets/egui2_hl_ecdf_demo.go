//go:build llm_generated_opus47

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
)

func init() {
	registry.Register(registry.Demo{
		Name:     "ecdf",
		Category: "Charts & plots",
		Title:    icons.IconChartLine + " ecdf",
		Stage:    [2]float32{960, 640},
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

	// --- Plot ------------------------------------------------------------
	method := ecdfDemoMethods[st.methodIdx].method
	alpha := ecdfDemoAlphas[st.alphaIdx]
	sample := ecdfDemoSamples[st.sampleIdx]

	r := ecdf.New().
		Method(method).
		Alpha(alpha).
		SeriesName(sample.name)

	// Absolute plot id so the ecdf widget can match its own r15 hover
	// register read against the c.Plot block's id. Using AbsoluteWidgetId
	// instead of ids.PrepareStr keeps the id stable independent of the
	// surrounding WidgetIdStack context.
	plotID := c.MakeAbsoluteIdStr("ecdf-plot")

	ch := r.At(plotID, sample.sorted)
	_ = r.Render(sample.sorted)
	r.PaintCrosshair(ch)

	// Panning is disabled via both AllowDrag(false) and
	// AllowScroll(false) — the band is bounded in y ∈ [0, 1]
	// and the data range bounds x, so there is nowhere
	// meaningful to pan to. Scroll defaults to [true, true]
	// in the interpreter, so leaving it implicit still lets
	// trackpad two-finger scroll translate the plot. Zoom
	// stays on — readers often want to inspect tail detail.
	//
	// ClampX / ClampY pin the outer viewport so zooming out
	// cannot strand the reader in empty space — ECDF support
	// is naturally [0, 1] on Y, and the sample's [min, max]
	// bounds X. Zooming in remains unrestricted.
	xLo, xHi := sample.sorted[0], sample.sorted[len(sample.sorted)-1]
	c.Plot(plotID).
		Width(900).Height(500).
		XAxisLabel("value").
		YAxisLabel("F(x)").
		Legend().
		AllowZoom(true).
		AllowDrag(false).
		AllowScroll(false).
		ShowGrid(true, true).
		IncludeY(0).IncludeY(1).
		ClampX(xLo, xHi).
		ClampY(0, 1).
		Send()

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
