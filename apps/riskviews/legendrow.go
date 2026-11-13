package riskviews

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// The prototype's legends are an inline, wrapping row of swatch + label
// pairs above the chart. widgets/legend draws a boxed vertical list inside a
// canvas (the plot-legend shape), so the row is painted here.

type swatchE uint8

const (
	swLine swatchE = iota
	swDot
	swRing
	swBox
	swDiamond
	swStatus
)

type legItem struct {
	kind   swatchE
	col    color.Color
	status StatusE
	label  string
}

func legendRow(ids *c.WidgetIdStack, key string, w float32, p palette, items []legItem) {
	const rowH, gap, sw = 20, 18, 16
	// lay out with wrapping
	type placed struct{ x, y float32 }
	pos := make([]placed, len(items))
	x, y := float32(0), float32(0)
	for i, it := range items {
		iw := sw + 6 + implot.EstimateTextWidth(it.label, fsLbl)
		if x > 0 && x+iw > w {
			x, y = 0, y+rowH
		}
		pos[i] = placed{x, y}
		x += iw + gap
	}
	h := y + rowH
	for range c.IdScope(ids.PrepareStr(key)) {
		for i, it := range items {
			px, cy := pos[i].x, pos[i].y+rowH/2
			switch it.kind {
			case swLine:
				c.PaintRectFilled(px, cy-1.5, px+14, cy+1.5, 2, it.col).Send()
			case swDot:
				c.PaintCircleFilled(px+5, cy, 4.5, it.col).Send()
			case swRing:
				c.PaintCircleStroke(px+5, cy, 4, it.col, 2).Send()
			case swBox:
				c.PaintRectFilled(px, cy-6, px+12, cy+6, 2, it.col).Send()
			case swDiamond:
				c.PaintPolygonFilled([]float32{px + 6, px + 11, px + 6, px + 1}, []float32{cy - 5, cy, cy + 5, cy}, it.col).Send()
			case swStatus:
				statusDot(px+8, cy, 8, it.status)
			}
			text(px+sw+6, cy, anchorStart, anchorCenter, it.label, fsLbl, p.ink2)
		}
		c.PaintCanvas(ids.PrepareStr("cv"), w, h).Background(p.bg).Send()
	}
}
