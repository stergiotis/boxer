//go:build llm_generated_opus47

package widgets

import (
	"math/rand"
	"sort"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/boxenplot"
)

// =============================================================================
// boxenplot widget demo — letter-value plots over four pre-built digests
//
// Four Gaussian variants (different μ/σ) are pre-fed into TDigests once
// at package init. The render loop fetches the LV ladder via
// letterval.RecommendedLevels and pipes each into the boxenplot.Renderer,
// laying out the four distributions side by side inside a single Plot.
// Live controls toggle outlier mode, palette, per-depth width shrink,
// and the Auto-mode threshold so the operator can compare visuals
// without restarting.
// =============================================================================

type boxenplotDemoState struct {
	outlierModeIdx int     // 0=Auto, 1=Points, 2=Count, 3=None
	accessIdx      int     // index into bpAccessOptions (Tier-2 preset)
	shrink         float64 // per-depth box width multiplier
	autoThreshold  uint64
}

type bpDist struct {
	name     string
	digest   *tdigest.TDigest
	extremes []float64 // bottom-K + top-K for OutlierModePoints
}

var (
	bpDistributions = buildBpDistributions()

	bpOutlierModes = []struct {
		label string
		mode  boxenplot.OutlierModeE
	}{
		{"Auto", boxenplot.OutlierModeAuto},
		{"Points", boxenplot.OutlierModePoints},
		{"Count", boxenplot.OutlierModeCount},
		{"None", boxenplot.OutlierModeNone},
	}

	bpAccessOptions = []struct {
		label  string
		access styletokens.AccessibilityE
	}{
		{"Default", styletokens.AccessibilityDefault},
		{"HighContrast", styletokens.AccessibilityHighContrast},
		{"Monochrome", styletokens.AccessibilityMonochrome},
	}
)

func init() {
	registry.Register(registry.Demo{
		Name:     "boxenplot",
		Category: "Charts & plots",
		Title:    icons.IconChartBar + " boxenplot",
		Stage:    [2]float32{1024, 700},
		Kind:     registry.DemoKindUX,
		Description: "Hofmann/Wickham/Kafadar letter-value plot composing the boxer tdigest + " +
			"letterval packages with the egui2 plotBoxes primitive. Four Gaussian variants pre-fed " +
			"into TDigests; live controls toggle outlier mode (Auto/Points/Count/None), IDS sequential " +
			"palette, per-depth width shrink, and the Auto threshold.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &boxenplotDemoState{
				outlierModeIdx: 0,
				accessIdx:      0,
				shrink:         0.85,
				autoThreshold:  20,
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoBoxenplot(ids, state.(*boxenplotDemoState))
		},
		SourceFunc: demoBoxenplot,
	})
}

func demoBoxenplot(ids *c.WidgetIdStack, st *boxenplotDemoState) {
	// --- Control row -----------------------------------------------------
	for range c.Horizontal().KeepIter() {
		// Outlier mode combo.
		c.Label("Outliers:").Send()
		c.AddSpace(padInner())
		currentMode := bpOutlierModes[st.outlierModeIdx].label
		for range c.ComboBox(ids.PrepareStr("bp-outlier-cb"),
			c.WidgetText().Text("mode").Keep(),
			c.WidgetText().Text(currentMode).Keep()).KeepIter() {
			for i, opt := range bpOutlierModes {
				selected := i == st.outlierModeIdx
				if c.Button(ids.PrepareSeq(uint64(0xB0E000+i)),
					c.Atoms().Text(opt.label).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.outlierModeIdx = i
				}
			}
		}
		c.AddSpace(gapSections())

		// Accessibility preset combo (Tier-2). Resolves to a palette
		// override applied to every IDS sequential consumer in the
		// running session — this demo just inherits whatever the
		// resolver returns. Default → batlow; HighContrast → batlowK
		// + alpha/range boost; Monochrome → grayC.
		c.Label("Accessibility:").Send()
		c.AddSpace(padInner())
		currentAccess := bpAccessOptions[st.accessIdx].label
		for range c.ComboBox(ids.PrepareStr("bp-access-cb"),
			c.WidgetText().Text("preset").Keep(),
			c.WidgetText().Text(currentAccess).Keep()).KeepIter() {
			for i, opt := range bpAccessOptions {
				selected := i == st.accessIdx
				if c.Button(ids.PrepareSeq(uint64(0xB0E100+i)),
					c.Atoms().Text(opt.label).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					st.accessIdx = i
				}
			}
		}
		c.AddSpace(gapSections())

		// Shrink slider.
		c.Label("Shrink:").Send()
		c.AddSpace(padInner())
		c.SliderF64(ids.PrepareStr("bp-shrink"), st.shrink, 0.40, 1.00).
			SendRespVal(&st.shrink)
		c.AddSpace(gapSections())

		// Auto threshold drag.
		c.Label("AutoThresh:").Send()
		c.AddSpace(padInner())
		c.DragValueU64(ids.PrepareStr("bp-thresh"), st.autoThreshold).
			Speed(1.0).
			SendRespVal(&st.autoThreshold)
	}
	c.Separator().Horizontal().Send()

	// --- Plot ------------------------------------------------------------
	mode := bpOutlierModes[st.outlierModeIdx].mode

	// Default-combo defers to New()'s env-resolved preset (so a user
	// who sets IDS_ACCESSIBILITY in the shell sees that preset honoured
	// when the combo is at Default). Non-default combos override
	// explicitly — the combo wins over env for this session.
	r := boxenplot.New("bp-demo").
		OutlierMode(mode).
		BoxWidth(0.6, st.shrink).
		OutlierAutoThreshold(int64(st.autoThreshold))

	switch bpAccessOptions[st.accessIdx].access {
	case styletokens.AccessibilityHighContrast:
		r = r.Palette(styletokens.SequentialBatlowK).
			PaletteRange(0.10, 0.95).
			FillAlpha(0xFF)
	case styletokens.AccessibilityMonochrome:
		r = r.Palette(styletokens.SequentialGrayC)
	}

	// Absolute plot id so the bottom-status-line lookup can match its
	// r15 hover register read against the c.Plot block's id (mirrors
	// the ecdf demo). Using AbsoluteWidgetId keeps the id stable
	// independent of the surrounding WidgetIdStack context.
	plotID := c.MakeAbsoluteIdStr("bp-plot")

	var ch boxenplot.Crosshair
	for i, d := range bpDistributions {
		arg := float64(i) + 1.0
		levels := letterval.RecommendedLevels(d.digest)
		rd := r.SeriesName(d.name)
		if maybe := rd.At(plotID, arg, d.name, levels); maybe.Valid {
			ch = maybe
		}
		rd.Render(arg, levels, d.extremes, -1)
	}
	r.PaintCrosshair(ch)

	c.Plot(plotID).
		Width(940).Height(460).
		XAxisLabel("distribution").
		YAxisLabel("value").
		Legend().
		AllowZoom(true).
		AllowDrag(true).
		ShowGrid(true, true).
		IncludeXRange(0.0, 5.0).
		Send()

	c.AddSpace(padInner())
	boxenplot.WriteStatusLine(ch)
}

// buildBpDistributions seeds four TDigests with synthetic Gaussian
// variants and pre-extracts the bottom-K + top-K extremes so
// OutlierModePoints has authentic data to draw. K=8 matches
// letterval.MinTailCount.
func buildBpDistributions() (out []*bpDist) {
	type variant struct {
		name string
		mu   float64
		sig  float64
		seed int64
	}
	variants := []variant{
		{"N(0,1)", 0.0, 1.0, 11},
		{"N(0,2)", 0.0, 2.0, 22},
		{"N(2,1)", 2.0, 1.0, 33},
		{"N(-1,0.5)", -1.0, 0.5, 44},
	}
	const n = 10_000
	const k = 8
	out = make([]*bpDist, 0, len(variants))
	for _, v := range variants {
		rnd := rand.New(rand.NewSource(v.seed))
		data := make([]float64, n)
		for i := range data {
			data[i] = v.mu + v.sig*rnd.NormFloat64()
		}
		d := tdigest.NewTDigest()
		for _, x := range data {
			d.Push(x)
		}
		sorted := make([]float64, len(data))
		copy(sorted, data)
		sort.Float64s(sorted)
		extremes := make([]float64, 0, 2*k)
		extremes = append(extremes, sorted[:k]...)
		extremes = append(extremes, sorted[len(sorted)-k:]...)
		out = append(out, &bpDist{
			name:     v.name,
			digest:   d,
			extremes: extremes,
		})
	}
	return
}
