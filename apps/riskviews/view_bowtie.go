package riskviews

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

const bowtieMinW = 1100

func (inst *App) renderBowtie(w float32, p palette) {
	ids := inst.ids
	legendRow(ids, "bt-legend", w, p, []legItem{
		{kind: swStatus, status: StatusGood, label: barrierLabel(StatusGood)},
		{kind: swStatus, status: StatusWarn, label: barrierLabel(StatusWarn)},
		{kind: swStatus, status: StatusCrit, label: barrierLabel(StatusCrit)},
		{kind: swStatus, status: StatusNone, label: barrierLabel(StatusNone)},
	})
	c.AddSpace(6)
	cw := max(w, bowtieMinW)
	// The prototype scrolls a 1100 px minimum sideways; so does this.
	for range c.ScrollArea().Hscroll(true).Vscroll(false).AutoShrink(false, true).KeepIter() {
		for range c.IdScope(ids.PrepareStr("bowtie")) {
			drawBowtie(ids, cw, p)
		}
	}
}

// wrapLines breaks s to fit maxPx at fs, using the house width estimate.
func wrapLines(s string, maxPx, fs float32) []string { return implot.Wrap(s, maxPx, fs) }

func paintWrapped(x, y float32, s string, maxPx, fs, lh float32, strong bool, p palette) {
	ls := wrapLines(s, maxPx, fs)
	y0 := y - float32(len(ls)-1)*lh/2
	col := p.ink2
	if strong {
		col = p.ink
	}
	for i, l := range ls {
		text(x, y0+float32(i)*lh, anchorStart, anchorCenter, l, fs, col)
	}
}

func drawBowtie(ids *c.WidgetIdStack, W float32, p palette) {
	const H = 430
	cv := newCanvas(ids, W, H, p)
	cx, cy := W/2, float32(215)
	topW, topH := float32(150), float32(70)
	xTop := cx - topW/2
	colW, g, curve, bh := float32(130), float32(14), float32(40), float32(56)
	barW := float32(int((xTop - colW - 3*g - curve) / 2))
	rowsY := []float32{85, 215, 345}
	ls1 := colW + g
	ls2 := ls1 + barW + g
	lend := ls2 + barW
	xC := W - colW
	rs2 := xC - g - barW
	rs1 := rs2 - g - barW
	mono(0, 8, anchorStart, anchorTop, "THREATS", fsAx, p.ink2)
	mono(ls1, 8, anchorStart, anchorTop, "PREVENTION BARRIERS, IN SERIES", fsAx, p.ink2)
	mono(rs1, 8, anchorStart, anchorTop, "MITIGATION BARRIERS, IN SERIES", fsAx, p.ink2)
	mono(W, 8, anchorEnd, anchorTop, "CONSEQUENCES", fsAx, p.ink2)
	// connectors first, so boxes sit on top
	for i := range bowThreats {
		y := rowsY[i]
		mx := (lend + xTop) / 2
		line(colW, y, lend, y, p.axis, 1.5)
		c.PaintCubicBezier(lend, y, mx, y, mx, cy, xTop, cy, p.axis, 1.5).Send()
	}
	for i := range bowCons {
		y := rowsY[i]
		x0 := xTop + topW
		mx := (x0 + rs1) / 2
		c.PaintCubicBezier(x0, cy, mx, cy, mx, y, rs1, y, p.axis, 1.5).Send()
		line(rs1, y, xC, y, p.axis, 1.5)
	}
	// top event: inverted ink box, as the prototype's --ink fill
	c.PaintRectFilled(xTop, cy-topH/2, xTop+topW, cy+topH/2, 12, p.ink).Send()
	text(cx, cy-8, anchorMiddle, anchorCenter, "Ransomware", fsBody, p.bg)
	text(cx, cy+9, anchorMiddle, anchorCenter, "encrypts core ERP", fsBody, p.bg)
	mono(cx, cy-topH/2-6, anchorMiddle, anchorBottom, "TOP EVENT", fsAx, p.ink2)
	cv.tipRect(xTop, cy-topH/2, xTop+topW, cy+topH/2, "Top event", bowTop)

	barrier := func(x, y float32, b Barrier) {
		c.PaintRectFilled(x, y-bh/2, x+barW, y+bh/2, 4, p.area).Send()
		c.PaintRectStroke(x, y-bh/2, x+barW, y+bh/2, 4, p.axis, 1).Send()
		c.PaintRectFilled(x, y-bh/2, x+4, y+bh/2, 2, statusFill(b.Status)).Send()
		statusDot(x+18, y, 8, b.Status)
		paintWrapped(x+32, y, b.Name, barW-38, fsLbl, 13.5, false, p)
		cv.tipRect(x, y-bh/2, x+barW, y+bh/2, b.Name, barrierLabel(b.Status), b.Detail)
	}
	node := func(x, y float32, s string) {
		c.PaintRectFilled(x, y-28, x+colW, y+28, 4, p.band[0]).Send()
		c.PaintRectStroke(x, y-28, x+colW, y+28, 4, p.border, 1).Send()
		paintWrapped(x+10, y, s, colW-18, fsLbl, 14, true, p)
	}
	for i, it := range bowThreats {
		node(0, rowsY[i], it.Text)
		slots := []float32{ls2}
		if len(it.Barriers) == 2 {
			slots = []float32{ls1, ls2}
		}
		for k, b := range it.Barriers {
			barrier(slots[k], rowsY[i], b)
		}
	}
	for i, it := range bowCons {
		node(xC, rowsY[i], it.Text)
		slots := []float32{rs1}
		if len(it.Barriers) == 2 {
			slots = []float32{rs1, rs2}
		}
		for k, b := range it.Barriers {
			barrier(slots[k], rowsY[i], b)
		}
	}
	cv.finish()
}
