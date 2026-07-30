package widgets

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// implotDemoState holds the demo's series so the slices stay valid across
// the frame (implot.Line does not copy) and stable across frames.
type implotDemoState struct {
	xs, sin, cos, damped []float64
}

// implotM3State carries the M3 scales demo's series.
type implotM3State struct {
	tXs, tYs         []float64
	eXs              []float64
	e1Ys, e2Ys, e3Ys []float64
}

// implotM4State carries the M4 heatmap/histogram demo's data.
type implotM4State struct {
	smallVals, bigVals []float64
	cmSmall, cmBig     *colormap.Config
	samples            []float64
}

// implotM2State carries the M2 item-breadth demo's series.
type implotM2State struct {
	barXs, barYs     []float64
	scXs, scYs       []float64
	stairXs, stairYs []float64
	stemXs, stemYs   []float64
	shXs, shYs       []float64
}

func init() {
	registry.Register(registry.Demo{
		Name:        "implot_m1",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M1 (axes / ticks / lines / pan-zoom)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M1 plot frame core: linear axes with located ticks, grid, clipped line series (NaN-split), drag pan, anchored wheel zoom, Shift+drag box-zoom, double-click fit.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotDemoState{}
			const n = 400
			st.xs = make([]float64, n)
			st.sin = make([]float64, n)
			st.cos = make([]float64, n)
			st.damped = make([]float64, n)
			for i := range n {
				x := float64(i) / float64(n-1) * 4 * math.Pi
				st.xs[i] = x
				st.sin[i] = math.Sin(x)
				st.cos[i] = 0.75 * math.Cos(x)
				st.damped[i] = math.Exp(-x/6) * math.Sin(3*x)
				// A NaN gap proves the split-at-NaN contract visibly.
				if i > 180 && i < 195 {
					st.damped[i] = math.NaN()
				}
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotDemoState)
			p := implot.Begin(ids, "waves", 620, 380)
			p.SetupAxes("x [rad]", "amplitude", implot.AxisFlagsNone, implot.AxisFlagsNone)
			p.SetupAxisLimits(implot.AxisX1, 0, 4*math.Pi, implot.CondOnce)
			p.SetupAxisLimits(implot.AxisY1, -1.25, 1.25, implot.CondOnce)
			p.Line("sin(x)", st.xs, st.sin)
			p.Line("0.75·cos(x)", st.xs, st.cos)
			p.Line("damped (NaN gap)", st.xs, st.damped)
			p.End()
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m2",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M2 (items / legend)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M2 item breadth on one plot: bars, scatter, stairs, stems, shaded, infinite reference lines — with a clickable legend (toggle visibility, hover to highlight).",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM2State{}
			const n = 24
			for i := range n {
				x := float64(i)
				st.barXs = append(st.barXs, x)
				st.barYs = append(st.barYs, 2.2+1.6*math.Sin(x/3.2)+0.6*math.Cos(x/1.1))
				st.stairXs = append(st.stairXs, x)
				st.stairYs = append(st.stairYs, 5.4+1.1*math.Sin(x/4.5))
				st.stemXs = append(st.stemXs, x)
				st.stemYs = append(st.stemYs, -1.2+0.8*math.Sin(x/2.1))
			}
			const m = 60
			for i := range m {
				x := float64(i) / float64(m-1) * 23.0
				st.scXs = append(st.scXs, x)
				st.scYs = append(st.scYs, 7.4+0.7*math.Sin(x/1.7)+0.25*math.Cos(x*1.3))
				st.shXs = append(st.shXs, x)
				st.shYs = append(st.shYs, 9.4+0.6*math.Sin(x/2.6))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM2State)
			p := implot.Begin(ids, "item breadth", 620, 400)
			p.SetupAxes("x", "value", implot.AxisFlagsNone, implot.AxisFlagsNone)
			p.SetupAxisLimits(implot.AxisX1, -0.8, 23.8, implot.CondOnce)
			p.SetupAxisLimits(implot.AxisY1, -2.6, 10.6, implot.CondOnce)
			p.Bars("bars", st.barXs, st.barYs, 0.66)
			p.Stairs("stairs", st.stairXs, st.stairYs)
			p.Stems("stems", st.stemXs, st.stemYs, -2.2)
			p.Scatter("scatter", st.scXs, st.scYs, implot.MarkerDiamond, 3.5)
			p.Shaded("shaded", st.shXs, st.shYs, 8.8)
			p.InfLinesH("mean ref", []float64{4.6})
			p.End()
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m3",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M3 (time / log scales)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M3 axis scales, two plots in one frame (the per-id R24 register at work): a Unix-seconds time axis with boundary-snapped ticks, and a log10 y axis with decade ticks turning exponentials into straight lines.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM3State{}
			// Fixed epoch keeps the tour capture deterministic.
			base := float64(1_780_000_000)
			const n = 200
			for i := range n {
				tt := base + float64(i)/float64(n-1)*48*3600
				st.tXs = append(st.tXs, tt)
				st.tYs = append(st.tYs, 42+9*math.Sin(float64(i)/11)+3*math.Cos(float64(i)/3.1))
				x := float64(i) / float64(n-1) * 10
				st.eXs = append(st.eXs, x)
				st.e1Ys = append(st.e1Ys, math.Exp(x*0.9))
				st.e2Ys = append(st.e2Ys, 40*math.Exp(x*0.35))
				st.e3Ys = append(st.e3Ys, 3000*math.Exp(-x*0.4))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM3State)
			p := implot.Begin(ids, "requests over 48 h", 620, 240)
			p.SetupAxes("", "rate", implot.AxisFlagsNone, implot.AxisFlagsNone)
			p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
			p.Line("rate", st.tXs, st.tYs)
			p.End()
			p2 := implot.Begin(ids, "log-scale growth", 620, 240)
			p2.SetupAxes("x", "value (log10)", implot.AxisFlagsNone, implot.AxisFlagsNone)
			p2.SetupAxisScale(implot.AxisY1, implot.ScaleLog10)
			p2.SetupAxisLimits(implot.AxisY1, 0.5, 20000, implot.CondOnce)
			p2.Line("e^0.9x", st.eXs, st.e1Ys)
			p2.Line("40·e^0.35x", st.eXs, st.e2Ys)
			p2.Line("3000·e^-0.4x", st.eXs, st.e3Ys)
			p2.End()
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m4",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M4 (heatmaps / histograms)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M4: a small heatmap on the rect-batch route and a 256×160 field on the paintImage texture route (Viridis and Inferno via the colormap widget), plus a Sturges-binned histogram.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM4State{}
			st.smallVals = make([]float64, 24*14)
			for r := range 14 {
				for cix := range 24 {
					st.smallVals[r*24+cix] = math.Sin(float64(cix)/3.5) * math.Cos(float64(r)/2.2)
				}
			}
			st.cmSmall = colormap.NewConfig(colormap.Viridis8, -1, 1)
			st.bigVals = make([]float64, 256*160)
			for r := range 160 {
				for cix := range 256 {
					x := float64(cix) / 32.0
					y := float64(r) / 24.0
					st.bigVals[r*256+cix] = math.Sin(x)*math.Cos(y) + 0.4*math.Sin(2.4*x+1.7*y)
				}
			}
			st.cmBig = colormap.NewConfig(colormap.Inferno8, -1.4, 1.4)
			// Deterministic LCG samples: a Gaussian-ish sum of uniforms.
			lcg := uint64(88172645463325252)
			next := func() float64 {
				lcg = lcg*6364136223846793005 + 1442695040888963407
				return float64(lcg>>11) / float64(1<<53)
			}
			for range 4000 {
				s := 0.0
				for range 6 {
					s += next()
				}
				st.samples = append(st.samples, s)
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM4State)
			p := implot.Begin(ids, "heatmaps: rect route / texture route", 620, 250)
			p.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
			p.SetupAxisLimits(implot.AxisX1, 0, 21, implot.CondOnce)
			p.SetupAxisLimits(implot.AxisY1, 0, 10, implot.CondOnce)
			p.Heatmap("small (336 rects)", st.smallVals, 14, 24, st.cmSmall, 0, 0, 10, 10)
			p.Heatmap("big (256x160 texture)", st.bigVals, 160, 256, st.cmBig, 11, 0, 21, 10)
			p.End()
			p2 := implot.Begin(ids, "histograms", 620, 250)
			p2.SetupAxes("value", "count", implot.AxisFlagsAutoFit, implot.AxisFlagsAutoFit)
			p2.Histogram("histogram", st.samples, 0, false)
			p2.End()
		},
	})
}
