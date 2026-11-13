package riskviews

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

func (inst *App) renderTolerance(w float32, p palette) {
	ids := inst.ids
	items := []legItem{}
	for _, s := range scenarios {
		items = append(items, legItem{kind: swDot, col: tok(slotColor(s.Slot)), label: s.Name})
	}
	items = append(items, legItem{kind: swLine, col: p.ink, label: "Impact tolerance"})
	legendRow(ids, "tol-legend", w, p, items)
	c.AddSpace(4)
	drawTolerance(ids, w, p)
}

// drawTolerance is an ImPlot-port plot on a log10 x axis. The service rows
// are a categorical y axis (SetupAxisTicks); the second label line ("2 of 3
// breach") has no place on a tick label, so it moves into the tick label
// itself, and the per-row tolerance band and marker are a Custom item.
func drawTolerance(ids *c.WidgetIdStack, W float32, p palette) {
	const rowH = 62
	n := len(services)
	H := float32(8+n*rowH+40) + 24
	for pl := range implot.Scoped(ids, "##tolerance", W, H) {
		pl.NoInputs().NoLegend()
		pl.SetupAxes("Estimated time to recover (log scale)", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
		pl.SetupAxisScale(implot.AxisX1, implot.ScaleLog10)
		pl.SetupAxisLimits(implot.AxisX1, 1, 336, implot.CondAlways)
		// rows top to bottom: service i sits at y = n-1-i
		pl.SetupAxisLimits(implot.AxisY1, -0.5, float64(n)-0.5, implot.CondAlways)
		xt := []float64{1, 4, 12, 24, 72, 168, 336}
		if W < 480 {
			xt = []float64{1, 12, 72, 336}
		}
		xl := make([]string, len(xt))
		for i, h := range xt {
			xl[i] = fmtHours(h)
		}
		pl.SetupAxisTicks(implot.AxisX1, xt, xl)
		yv, yl := make([]float64, n), make([]string, n)
		for i, s := range services {
			over := 0
			for _, e := range s.Est {
				if e > s.Tol {
					over++
				}
			}
			yv[i] = float64(n - 1 - i)
			if over > 0 {
				yl[i] = s.Name + " · " + strconv.Itoa(over) + " of 3 breach"
			} else {
				yl[i] = s.Name + " · all within"
			}
		}
		pl.SetupAxisTicks(implot.AxisY1, yv, yl)
		crit := tokA(styletokens.ErrorDefault, 0x18)
		ink := p.ink
		pl.Custom("##bands", func(d implot.DrawCtx) {
			for i, s := range services {
				yc := d.T.PxY(float64(n - 1 - i))
				x := d.T.PxX(s.Tol)
				c.PaintRectFilled(x, yc-20, d.AreaX+d.AreaW, yc+20, 0, crit).Send()
				line(x, yc-20, x, yc+20, ink, 2)
			}
		})
		bg := p.bg
		for k, sc := range scenarios {
			col, scName := tok(slotColor(sc.Slot)), sc.Name
			pl.Custom(sc.Name, func(d implot.DrawCtx) {
				for i, s := range services {
					e := s.Est[k]
					x := d.T.PxX(e)
					y := d.T.PxY(float64(n-1-i)) + float32(k-1)*9
					c.PaintCircleFilled(x, y, 5.5, col).Send()
					c.PaintCircleStroke(x, y, 5.5, bg, 2).Send()
					tl := "Tolerance " + fmtHours(s.Tol)
					if e > s.Tol {
						tl += " · breached"
					}
					c.PaintHitCircle(d.Ids.PrepareSeq(uint64(i)), x, y, 7.5).
						Label(s.Name + " · " + scName).Tooltip("Recovery about " + fmtHoursTip(e) + "\n" + tl).Send()
				}
			})
		}
	}
}
