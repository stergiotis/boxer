package riskviews

import (
	"math"
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func (inst *App) renderMatrix(w float32, p palette) {
	ids := inst.ids
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("mx-cells"), c.Atoms().Text("Banded 5×5").Keep()).Selected(!inst.mxCont).SendResp().HasPrimaryClicked() {
			inst.mxCont = false
		}
		if c.Button(ids.PrepareStr("mx-cont"), c.Atoms().Text("Continuous with ranges").Keep()).Selected(inst.mxCont).SendResp().HasPrimaryClicked() {
			inst.mxCont = true
		}
		c.AddSpace(12)
		c.Checkbox(ids.PrepareStr("mx-inh"), inst.mxInh, "Show inherent → residual").SendRespVal(&inst.mxInh)
	}
	c.AddSpace(6)
	for range c.IdScope(ids.PrepareStr("matrix")) {
		drawMatrix(ids, w, p, inst.mxCont, inst.mxInh)
	}
	c.AddSpace(6)
	for range c.CollapsingHeader(ids.PrepareStr("reg-table"), c.WidgetText().Text("Register as a table").Keep()).KeepIter() {
		for range c.Grid(ids.PrepareStr("reg-grid")).NumColumns(6).Striped(true).KeepIter() {
			for _, h := range []string{"ID", "Risk", "Likelihood / yr (residual)", "Impact (residual)", "Likelihood (inherent)", "Impact (inherent)"} {
				c.LabelAtoms(c.Atoms().BeginRichText(h).Strong().End().Keep()).Send()
			}
			c.EndRow()
			for _, r := range risks {
				c.Label(r.Id).Send()
				c.Label(r.Name).Send()
				c.LabelAtoms(c.Atoms().BeginRichText(pct(r.P[0], 1) + "–" + pct(r.P[1], 0)).Monospace().End().Keep()).Send()
				c.LabelAtoms(c.Atoms().BeginRichText(eur(r.I[0]) + "–" + eur(r.I[1])).Monospace().End().Keep()).Send()
				c.LabelAtoms(c.Atoms().BeginRichText(pct(r.IP[0], 1) + "–" + pct(r.IP[1], 0)).Monospace().End().Keep()).Send()
				c.LabelAtoms(c.Atoms().BeginRichText(eur(r.II[0]) + "–" + eur(r.II[1])).Monospace().End().Keep()).Send()
				c.EndRow()
			}
		}
	}
}

type riskPos struct {
	r        Risk
	res, inh [2]float32
}

func drawMatrix(ids *c.WidgetIdStack, W float32, p palette, cont, showInh bool) {
	narrow := W < 620
	H := float32(470)
	ml, mr := float32(112), float32(230)
	if narrow {
		H, ml, mr = 420, 92, 10
	}
	// The y-axis title sits above the axis: the painter has no rotated text.
	mt, mb := float32(30), float32(62)
	cv := newCanvas(ids, W, H, p)
	pw, ph := W-ml-mr, H-mt-mb
	var rp []riskPos
	s2 := p.s[1]
	if !cont {
		wts := []float32{1, 1.15, 1.32, 1.52, 1.75}
		sw := float32(0)
		for _, v := range wts {
			sw += v
		}
		xs, ys := []float32{ml}, []float32{mt + ph}
		for i, v := range wts {
			xs = append(xs, xs[i]+pw*v/sw)
			ys = append(ys, ys[i]-ph*v/sw)
		}
		for li := 0; li < 5; li++ {
			for ii := 0; ii < 5; ii++ {
				sh := cellShade(li, ii)
				c.PaintRectFilled(xs[ii]+1, ys[li+1]+1, xs[ii+1]-1, ys[li]-1, 3, p.seq[sh]).Send()
				cv.tipRect(xs[ii], ys[li+1], xs[ii+1], ys[li],
					likeLabels[li]+" × "+impLabels[ii],
					"Expected loss at centre "+cellShadeLabels[sh])
			}
		}
		for a := 0; a < 5; a++ {
			l := likeLabels[a]
			if narrow {
				l = strings.Replace(l, " yr", "", 1)
			}
			mono(ml-8, (ys[a]+ys[a+1])/2, anchorEnd, anchorCenter, l, fsAx, p.ink2)
			mono((xs[a]+xs[a+1])/2, mt+ph+12, anchorMiddle, anchorTop, impLabels[a], fsAx, p.ink2)
		}
		posCell := func(pr, imp float64) [2]float32 {
			li, ii := bandIdx(likeBands, pr), bandIdx(impBands, imp)
			fy := (math.Log(pr) - math.Log(likeBands[li])) / (math.Log(likeBands[li+1]) - math.Log(likeBands[li]))
			fx := (math.Log(imp) - math.Log(impBands[ii])) / (math.Log(impBands[ii+1]) - math.Log(impBands[ii]))
			fx = math.Min(0.85, math.Max(0.15, fx))
			fy = math.Min(0.85, math.Max(0.15, fy))
			return [2]float32{xs[ii] + float32(fx)*(xs[ii+1]-xs[ii]), ys[li] - float32(fy)*(ys[li]-ys[li+1])}
		}
		for _, r := range risks {
			rp = append(rp, riskPos{r: r, res: posCell(gmean(r.P), gmean(r.I)), inh: posCell(gmean(r.IP), gmean(r.II))})
		}
	} else {
		xS := logScale(10e3, 100e6, ml, ml+pw)
		yS := logScale(0.001, 1, mt+ph, mt)
		c.PaintRectFilled(ml, mt, ml+pw, mt+ph, 0, p.area).Send()
		for _, v := range []float64{10e3, 100e3, 1e6, 10e6, 100e6} {
			line(xS(v), mt, xS(v), mt+ph, p.grid, 1)
			mono(xS(v), mt+ph+12, anchorMiddle, anchorTop, eur(v), fsAx, p.ink2)
		}
		for _, v := range []float64{0.001, 0.01, 0.1, 1} {
			line(ml, yS(v), ml+pw, yS(v), p.grid, 1)
			l := "every yr"
			if v != 1 {
				l = strings.Replace(oneIn(v), " yr", "", 1) + " yr"
			}
			mono(ml-8, yS(v), anchorEnd, anchorCenter, l, fsAx, p.ink2)
		}
		c.PaintClipPush(ml, mt, ml+pw, mt+ph).Send()
		for _, e := range []float64{10e3, 100e3, 1e6} {
			line(xS(e/1), yS(1), xS(e/0.001), yS(0.001), p.axis, 1)
		}
		boxFill, boxStroke := tokA(slotColor(slotResidual), 0x1a), tokA(slotColor(slotResidual), 0x8c)
		for _, r := range risks {
			x0, x1 := xS(r.I[0]), xS(r.I[1])
			y0, y1 := yS(r.P[1]), yS(r.P[0])
			c.PaintRectFilled(x0, y0, x1, y1, 2, boxFill).Send()
			c.PaintRectStroke(x0, y0, x1, y1, 2, boxStroke, 1).Send()
			rp = append(rp, riskPos{r: r,
				res: [2]float32{xS(gmean(r.I)), yS(gmean(r.P))},
				inh: [2]float32{xS(gmean(r.II)), yS(gmean(r.IP))}})
		}
		c.PaintClipPop().Send()
		for _, e := range []float64{10e3, 100e3, 1e6} {
			lx := math.Min(e/0.004, 60e6)
			ly := e / lx
			mono(xS(lx)+4, yS(ly)-4, anchorStart, anchorBottom, eur(e)+"/yr", fsAx, p.ink2)
		}
		if !narrow {
			mono(ml+pw-4, mt+8, anchorEnd, anchorTop, "Diagonal lines: equal expected annual loss", fsAx, p.ink2)
		}
	}
	text(ml+pw/2, H-8, anchorMiddle, anchorBottom, "Impact if it happens (90% range midpoint)", fsLbl, p.ink2)
	text(4, 4, anchorStart, anchorTop, "Likelihood per year", fsLbl, p.ink2)

	if showInh {
		for _, q := range rp {
			dx, dy := q.res[0]-q.inh[0], q.res[1]-q.inh[1]
			d := float32(math.Hypot(float64(dx), float64(dy)))
			c.PaintCircleFilled(q.inh[0], q.inh[1], 5, p.bg).Send()
			c.PaintCircleStroke(q.inh[0], q.inh[1], 5, p.ink2, 1.5).Send()
			if d > 14 {
				ox, oy := q.inh[0]+dx/d*6, q.inh[1]+dy/d*6
				ex, ey := q.res[0]-dx/d*9, q.res[1]-dy/d*9
				c.PaintArrow(ox, oy, ex-ox, ey-oy, p.ink2, 1.5).Send()
			}
		}
	}
	for _, q := range rp {
		r := q.r
		c.PaintCircleFilled(q.res[0], q.res[1], 6.5, s2).Send()
		c.PaintCircleStroke(q.res[0], q.res[1], 6.5, p.bg, 2).Send()
		mono(q.res[0]+10, q.res[1], anchorStart, anchorCenter, r.Id, fsAx+0.5, p.ink)
		// the risk dots are Tab stops; the cells are not
		c.PaintHitCircle(cv.next(), q.res[0], q.res[1], 8.5).Label(r.Id+" · "+r.Name).
			Tooltip("Likelihood "+pct(r.P[0], 1)+"–"+pct(r.P[1], 0)+" / yr\n"+
				"Impact "+eur(r.I[0])+"–"+eur(r.I[1])+"\n"+
				"Inherent "+pct(r.IP[0], 1)+"–"+pct(r.IP[1], 0)+", "+eur(r.II[0])+"–"+eur(r.II[1])).
			Focusable().Send()
	}
	if !narrow {
		kx, ky := W-mr+24, mt+6
		text(kx, ky, anchorStart, anchorCenter, "Residual risks", fsBody, p.ink)
		for i, r := range risks {
			y := ky + 22 + float32(i)*19
			mono(kx, y, anchorStart, anchorCenter, r.Id, fsAx+0.5, p.ink)
			text(kx+28, y, anchorStart, anchorCenter, r.Name, fsLbl, p.ink2)
		}
		y0 := ky + 22 + 8*19 + 14
		if !cont {
			text(kx, y0, anchorStart, anchorCenter, "Cell shade", fsBody, p.ink)
			text(kx, y0+18, anchorStart, anchorCenter, "Expected annual loss at the cell", fsLbl, p.ink2)
			text(kx, y0+34, anchorStart, anchorCenter, "centre, one blue ramp:", fsLbl, p.ink2)
			for i := range 5 {
				yy := y0 + 46 + float32(i)*18
				c.PaintRectFilled(kx, yy, kx+14, yy+12, 2, p.seq[i]).Send()
				c.PaintRectStroke(kx, yy, kx+14, yy+12, 2, p.border, 1).Send()
				mono(kx+22, yy+6, anchorStart, anchorCenter, cellShadeLabels[i], fsAx, p.ink2)
			}
		} else {
			text(kx, y0, anchorStart, anchorCenter, "Boxes", fsBody, p.ink)
			text(kx, y0+18, anchorStart, anchorCenter, "90% ranges for likelihood", fsLbl, p.ink2)
			text(kx, y0+34, anchorStart, anchorCenter, "and impact. Dots mark the", fsLbl, p.ink2)
			text(kx, y0+50, anchorStart, anchorCenter, "geometric midpoint.", fsLbl, p.ink2)
		}
	}
	cv.finish()
}
