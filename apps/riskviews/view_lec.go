package riskviews

import (
	"math"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// lecCurves caches the three curves: the samples never change.
var lecXs, lecInh, lecRes, lecTol = func() (xs, inh, res, tl []float64) {
	xs = logSpace(50e3, 80e6, 120)
	for _, x := range xs {
		inh = append(inh, math.Min(1, exceed(simInh, x)))
		res = append(res, math.Min(1, exceed(simRes, x)))
		tl = append(tl, math.Min(1, tolAt(x)))
	}
	return
}()

func (inst *App) renderLEC(w float32, p palette) {
	ids := inst.ids
	legendRow(ids, "lec-legend", w, p, []legItem{
		{kind: swLine, col: p.s[0], label: "Inherent (no controls)"},
		{kind: swLine, col: p.s[1], label: "Residual (with controls)"},
		{kind: swLine, col: p.ink2, label: "Board tolerance"},
	})
	c.AddSpace(4)
	crit := hexA(styletokens.ErrorDefault, 0x30)
	for pl := range implot.Scoped(ids, "##lec", w, 300) {
		pl.NoInputs().NoLegend()
		pl.SetupAxes("Total annual loss at least (log scale)", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
		pl.SetupAxisScale(implot.AxisX1, implot.ScaleLog10)
		pl.SetupAxisLimits(implot.AxisX1, 50e3, 80e6, implot.CondAlways)
		pl.SetupAxisLimits(implot.AxisY1, 0, 1, implot.CondAlways)
		xt := []float64{100e3, 1e6, 10e6}
		if w > 480 {
			xt = []float64{100e3, 300e3, 1e6, 3e6, 10e6, 30e6}
		}
		xl := make([]string, len(xt))
		for i, v := range xt {
			xl[i] = eur(v)
		}
		pl.SetupAxisTicks(implot.AxisX1, xt, xl)
		yt := []float64{0, 0.2, 0.4, 0.6, 0.8, 1}
		yl := make([]string, len(yt))
		for i, v := range yt {
			yl[i] = pct(v, 0)
		}
		pl.SetupAxisTicks(implot.AxisY1, yt, yl)

		// breach shading: one ShadedBetween per contiguous run where the
		// residual curve sits above the tolerance curve
		var sx, sr, st []float64
		flush := func() {
			if len(sx) > 1 {
				pl.SetNextColor(crit).ShadedBetween("##breach", sx, sr, st)
			}
			sx, sr, st = nil, nil, nil
		}
		for i, x := range lecXs {
			if lecRes[i] > lecTol[i] {
				sx, sr, st = append(sx, x), append(sr, lecRes[i]), append(st, lecTol[i])
			} else {
				flush()
			}
		}
		flush()
		pl.SetNextColor(p.sHex[0]).SetNextWeight(2).Line("Inherent (no controls)", lecXs, lecInh)
		pl.SetNextColor(styletokens.NeutralTextSecondary.AsHex()).SetNextWeight(2).Line("Board tolerance", lecXs, lecTol)
		pl.SetNextColor(p.sHex[1]).SetNextWeight(2).Line("Residual (with controls)", lecXs, lecRes)
		var tx, ty []float64
		for _, t := range tol {
			if t[0] >= 50e3 && t[0] <= 80e6 {
				tx, ty = append(tx, t[0]), append(ty, t[1])
			}
		}
		pl.SetNextColor(styletokens.NeutralTextSecondary.AsHex()).Scatter("Tolerance points", tx, ty, implot.MarkerCircle, 4)

		// crosshair + read-out, from last frame's hover; the read-out is the
		// tooltip of a hit region over the plot area
		var readout []string
		if hx, _, ok := pl.HoverPlotPos(); ok {
			pi, pr, t := exceed(simInh, hx), exceed(simRes, hx), tolAt(hx)
			bg := p.bg
			pl.Custom("##cross", func(d implot.DrawCtx) {
				px := d.T.PxX(hx)
				line(px, d.AreaY, px, d.AreaY+d.AreaH, p.axis, 1)
				for _, q := range []struct {
					v   float64
					col color.Color
				}{{pi, p.s[0]}, {pr, p.s[1]}} {
					py := d.T.PxY(math.Min(1, q.v))
					c.PaintCircleFilled(px, py, 4.5, q.col).Send()
					c.PaintCircleStroke(px, py, 4.5, bg, 2).Send()
				}
			})
			readout = []string{"Loss ≥ " + eur(hx) + " in a year", "Inherent " + pct(pi, 1), "Residual " + pct(pr, 1), "Tolerance " + pct(t, 1)}
			if pr > t {
				readout[3] += " · exceeded"
			}
		}
		pl.Custom("##readout", func(d implot.DrawCtx) {
			c.PaintHitRect(d.Ids.PrepareSeq(0), d.AreaX, d.AreaY, d.AreaX+d.AreaW, d.AreaY+d.AreaH).
				Label("Loss exceedance").Tooltip(strings.Join(readout, "\n")).Send()
		})
	}
	c.AddSpace(6)
	if lo, hi, ok := lecBreach(); ok {
		callout(p, "Residual exceeds tolerance between "+eur(lo)+" and "+eur(hi)+".",
			" At €5m the residual chance is "+pct(exceed(simRes, 5e6), 1)+" a year against a tolerance of "+pct(tolAt(5e6), 1)+".")
	} else {
		callout(p, "Residual stays inside tolerance across the range.", "")
	}
}
