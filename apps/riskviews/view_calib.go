package riskviews

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

func (inst *App) renderCalib(w float32, p palette) {
	ids := inst.ids
	items := []legItem{}
	for _, s := range calib {
		items = append(items, legItem{kind: swDot, col: tok(slotColor(s.Slot)), label: s.Name})
	}
	items = append(items, legItem{kind: swLine, col: p.axis, label: "Perfect calibration"})
	legendRow(ids, "cal-legend", w, p, items)
	c.AddSpace(4)
	S := min(w, 420)
	for range c.HorizontalTop().KeepIter() {
		c.AddSpace((w - S) / 2)
		drawCalib(ids, S, p)
	}
	c.AddSpace(6)
	callout(p, "Brier score "+strconv.FormatFloat(brier(calib[0]), 'f', 3, 64)+" → "+strconv.FormatFloat(brier(calib[1]), 'f', 3, 64),
		" (lower is better). The 2024 estimates were overconfident at both ends.")
}

func calRadius(n float64) float32 { return float32(3 + math.Sqrt(n)*0.8) }

func drawCalib(ids *c.WidgetIdStack, S float32, p palette) {
	for pl := range implot.Scoped(ids, "##calib", S, S) {
		pl.NoInputs().NoLegend()
		pl.SetupAxes("Estimated likelihood", "Share that happened", implot.AxisFlagsNone, implot.AxisFlagsNone)
		pl.SetupAxisLimits(implot.AxisX1, 0, 1, implot.CondAlways)
		pl.SetupAxisLimits(implot.AxisY1, 0, 1, implot.CondAlways)
		ts := []float64{0, 0.25, 0.5, 0.75, 1}
		ls := make([]string, len(ts))
		for i, v := range ts {
			ls[i] = pct(v, 0)
		}
		pl.SetupAxisTicks(implot.AxisX1, ts, ls)
		pl.SetupAxisTicks(implot.AxisY1, ts, ls)
		pl.SetNextColor(styletokens.NeutralBorderFaint.AsHex()).SetNextWeight(1.5).Line("Perfect calibration", []float64{0, 1}, []float64{0, 1})
		for _, s := range calib {
			xs, ys := make([]float64, len(s.Pts)), make([]float64, len(s.Pts))
			for i, q := range s.Pts {
				xs[i], ys[i] = q[0], q[1]
			}
			// ImPlot's Scatter takes one radius per series; the prototype
			// sizes each point by its count, so the points are a Custom item.
			pl.SetNextColor(hexA(slotColor(s.Slot), 0x8c)).SetNextWeight(2).Line(s.Name+"##line", xs, ys)
			col, bg := tok(slotColor(s.Slot)), p.bg
			pts, name := s.Pts, s.Name
			pl.Custom(s.Name+"##pts", func(d implot.DrawCtx) {
				for i, q := range pts {
					x, y, r := d.T.PxX(q[0]), d.T.PxY(q[1]), calRadius(q[2])
					c.PaintCircleFilled(x, y, r, col).Send()
					c.PaintCircleStroke(x, y, r, bg, 2).Send()
					c.PaintHitCircle(d.Ids.PrepareSeq(uint64(i)), x, y, r+2).
						Label(name + " · estimated " + pct(q[0], 0)).
						Tooltip(strconv.Itoa(int(q[2])) + " risks estimated near " + pct(q[0], 0) + "\n" +
							strconv.Itoa(int(jsRound(q[1]*q[2]))) + " of them happened (" + pct(q[1], 0) + ")").
						Send()
				}
			})
		}
	}
}
