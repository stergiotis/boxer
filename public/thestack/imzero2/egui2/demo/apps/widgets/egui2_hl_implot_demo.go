package widgets

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// implotDemoState holds the demo's series so the slices stay valid across
// the frame (implot.Line does not copy) and stable across frames.
type implotDemoState struct {
	xs, sin, cos, damped []float64
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
}
