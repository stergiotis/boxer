package widgets

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// implotColorbarState pairs an implot heatmap with a colorscale legend on
// one shared colormap.Config — the visible half of ADR-0149 M4's colormap
// integration (implot-adoption survey, P1). The Config is the only
// coupling: both ends read its palette and range, so they cannot drift.
type implotColorbarState struct {
	vals []float64
	cm   *colormap.Config
	cs   *colorscale.ColorScale
}

const (
	implotCbRows = 18
	implotCbCols = 28
)

func init() {
	registry.Register(registry.Demo{
		Name:        "implot_colorbar",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot + colorscale colorbar",
		Stage:       [2]float32{700, 470},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "A heatmap and its colorbar legend sharing one colormap.Config — plot and legend stay in lock-step by construction. Hovering the colorbar dims heatmap cells outside the hovered value band (the colorscale.HoverBand idiom, expressed as a Custom overlay); hovering the heatmap reads the cell value back out through HoverPlotPos.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			st := &implotColorbarState{}
			st.vals = make([]float64, implotCbRows*implotCbCols)
			for r := range implotCbRows {
				for cix := range implotCbCols {
					x := float64(cix) / 4.5
					y := float64(r) / 3.2
					st.vals[r*implotCbCols+cix] = math.Sin(x)*math.Cos(y) + 0.35*math.Sin(1.9*x-1.3*y)
				}
			}
			st.cm = colormap.NewConfig(colormap.Viridis8, -1.35, 1.35)
			st.cs = colorscale.New(ids, "implot-colorbar-cs", st.cm,
				colorscale.WithOrientation(colorscale.OrientationVertical),
				colorscale.WithSize(64, 360))
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotColorbarState)
			hov := st.cs.HoveredValue()
			cellVal, cellOk := 0.0, false
			for range c.Horizontal().KeepIter() {
				for p := range implot.Scoped(ids, "field##colorbar", 560, 360) {
					p.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
					p.SetupAxisLimits(implot.AxisX1, 0, implotCbCols, implot.CondOnce)
					p.SetupAxisLimits(implot.AxisY1, 0, implotCbRows, implot.CondOnce)
					p.Heatmap("field", st.vals, implotCbRows, implotCbCols, st.cm, 0, 0, implotCbCols, implotCbRows)
					if hov.Ok {
						lo, hi := st.cm.Range()
						band := (hi - lo) * 0.07
						p.Custom("", func(dc implot.DrawCtx) {
							// Row 0 sits at the TOP edge (the Heatmap
							// orientation contract): cell (r, cix) spans
							// plot-y [rows-r-1, rows-r].
							for r := range implotCbRows {
								for cix := range implotCbCols {
									if math.Abs(st.vals[r*implotCbCols+cix]-hov.Value) <= band {
										continue
									}
									x0 := dc.T.PxX(float64(cix))
									x1 := dc.T.PxX(float64(cix + 1))
									y0 := dc.T.PxY(float64(implotCbRows - r))
									y1 := dc.T.PxY(float64(implotCbRows - r - 1))
									c.PaintRectFilled(x0, y0, x1, y1, 0, color.Hex(0x111318b8)).Send()
								}
							}
						})
					}
					if hx, hy, ok := p.HoverPlotPos(); ok {
						cix := int(math.Floor(hx))
						r := implotCbRows - 1 - int(math.Floor(hy))
						if cix >= 0 && cix < implotCbCols && r >= 0 && r < implotCbRows {
							cellVal, cellOk = st.vals[r*implotCbCols+cix], true
						}
					}
				}
				st.cs.Render()
			}
			readout := "hover the heatmap for a cell value, the colorbar to band-highlight"
			if cellOk {
				readout = fmt.Sprintf("cell value %.3f", cellVal)
			}
			if hov.Ok {
				readout += fmt.Sprintf(" · colorbar band around %.3f", hov.Value)
			}
			c.LabelAtoms(c.Atoms().Text(readout).Keep()).Send()
		},
	})
}
