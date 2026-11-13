package riskviews

import (
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

func (inst *App) renderTimeline(w float32, p palette) {
	ids := inst.ids
	legendRow(ids, "tl-legend", w, p, []legItem{
		{kind: swDot, col: p.s[0], label: "Incident, area ∝ loss"},
		{kind: swRing, col: p.s[0], label: "Near miss"},
		{kind: swLine, col: p.axis, label: "Control change"},
	})
	c.AddSpace(4)
	for range c.IdScope(ids.PrepareStr("timeline")) {
		drawTimeline(ids, w, p)
	}
}

// drawTimeline is hand-painted. widgets/timeline draws intervals and points
// on a time axis in lanes, but not area-scaled markers, hollow near-miss
// rings or labelled cross-lane event rules, so the prototype's encoding
// would have been lost.
func drawTimeline(ids *c.WidgetIdStack, W float32, p palette) {
	const laneH = 54
	ml := float32(100)
	if W < 480 {
		ml = 84
	}
	mr, mt, mb := float32(16), float32(60), float32(30)
	H := mt + float32(len(lanes))*laneH + mb
	cv := newCanvas(ids, W, H, p)
	xS := linScale(0, 24, ml, W-mr)
	for i, l := range lanes {
		y := mt + float32(i)*laneH
		line(ml, y+laneH, W-mr, y+laneH, p.grid, 1)
		text(ml-10, y+laneH/2, anchorEnd, anchorCenter, l, fsLbl, p.ink2)
	}
	for i, t := range monthLabels {
		if W < 480 && i%2 == 1 {
			continue
		}
		x := xS(float64(i * 3))
		mono(x, H-4, anchorMiddle, anchorBottom, t, fsAx, p.ink2)
		line(x, H-mb, x, H-mb+4, p.axis, 1)
	}
	for i, cc := range controlChanges {
		x := xS(cc.Month)
		line(x, mt-46+float32(i)*14, x, H-mb, p.axis, 1.5)
		endA := x+implot.EstimateTextWidth(cc.Text, fsAx) > W-mr
		if endA {
			mono(x-4, mt-38+float32(i)*14, anchorEnd, anchorBottom, cc.Text, fsAx, p.ink2)
		} else {
			mono(x+4, mt-38+float32(i)*14, anchorStart, anchorBottom, cc.Text, fsAx, p.ink2)
		}
	}
	fill := tokA(slotColor(slotInherent), 0xd9)
	for _, d := range incidents {
		x := xS(d.Month)
		y := mt + float32(d.Lane)*laneH + laneH/2
		if d.NearMiss {
			c.PaintCircleFilled(x, y, 5, p.bg).Send()
			c.PaintCircleStroke(x, y, 5, p.s[0], 2).Send()
			cv.tipCircle(x, y, 5, d.Text, lanes[d.Lane]+" · near miss")
		} else {
			r := float32(math.Max(4.5, math.Sqrt(d.Loss/520e3)*17))
			c.PaintCircleFilled(x, y, r, fill).Send()
			c.PaintCircleStroke(x, y, r, p.bg, 2).Send()
			cv.tipCircle(x, y, r, d.Text, lanes[d.Lane]+" · loss "+eur(d.Loss))
		}
	}
	cv.finish()
}
