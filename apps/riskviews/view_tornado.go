package riskviews

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

var tornadoBase, tornadoRows = tornadoItems()

func (inst *App) renderTornado(w float32, p palette) {
	ids := inst.ids
	legendRow(ids, "tn-legend", w, p, []legItem{
		{kind: swBox, col: p.divLo, label: "Input at its low end"},
		{kind: swBox, col: p.divHi, label: "Input at its high end"},
	})
	c.AddSpace(4)
	for range c.IdScope(ids.PrepareStr("tornado")) {
		drawTornado(ids, w, p)
	}
}

// drawTornado is hand-painted: the ImPlot port has vertical Bars only, no
// horizontal bars (upstream's PlotBarsH).
func drawTornado(ids *c.WidgetIdStack, W float32, p palette) {
	items, base := tornadoRows, tornadoBase
	const rowH = 30
	ml, mr, mt, mb := float32(112), float32(64), float32(32), float32(34)
	H := mt + float32(len(items))*rowH + mb
	cv := newCanvas(ids, W, H, p)
	lo, hi := items[0].Lo, items[0].Hi
	for _, it := range items {
		lo, hi = min(lo, it.Lo), max(hi, it.Hi)
	}
	pad := (hi - lo) * 0.05
	xS := linScale(lo-pad, hi+pad, ml, W-mr)
	line(xS(base), mt-10, xS(base), H-mb+4, p.ink2, 1.5)
	text(xS(base), mt-14, anchorMiddle, anchorBottom, "Base "+eur(base)+" a year", fsLbl, p.ink2)
	for i, it := range items {
		y := mt + float32(i)*rowH + rowH/2
		text(ml-10, y, anchorEnd, anchorCenter, it.Label, fsLbl, p.ink2)
		c.PaintRectFilled(xS(it.Lo), y-10, xS(it.Lo)+max(1, xS(base)-xS(it.Lo)-1), y+10, 3, p.divLo).Send()
		c.PaintRectFilled(xS(base)+1, y-10, xS(base)+1+max(1, xS(it.Hi)-xS(base)-1), y+10, 3, p.divHi).Send()
		mono(xS(it.Hi)+6, y, anchorStart, anchorCenter, eur(it.Hi), fsAx+0.5, p.ink)
		cv.tipRect(ml, y-rowH/2, W-mr, y+rowH/2, it.Label+" · "+it.Name, "Range "+it.Range, "Expected annual loss "+eur(it.Lo)+" → "+eur(it.Hi))
	}
	text(ml, H-6, anchorStart, anchorBottom, "Expected annual loss, residual (top 8 of 16 inputs)", fsLbl, p.ink2)
	cv.finish()
}
