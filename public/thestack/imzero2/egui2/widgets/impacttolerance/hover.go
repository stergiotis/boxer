package impacttolerance

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// mark is a hit region declared over what it covers: an estimate (a circle)
// or a service's tolerance line (a rect). label is its accessible name and
// the tooltip's first line, tip the tooltip's body. point is the estimate,
// NoPoint for a tolerance line, whose service is service.
type mark struct {
	point          Point
	service        int
	circle         bool
	cx, cy, r      float32
	x0, y0, x1, y1 float32
	label, tip     string
}

// pointSlack widens an estimate's region past its painted radius, so a small
// point is easy to aim at.
const pointSlack float32 = 2

// declareRegions emits the frame's hit regions (ADR-0149's hit regions) and
// returns, from their flags of the previous frame, the estimate hovered and
// the one clicked, NoPoint for none. The host hit-tests them, shows the
// winner's tooltip at the pointer and gives each an accessibility node named
// by its label; the estimates are also Tab stops, and Enter or Space on a
// focused one comes back as its click.
//
// The regions go out before the paint commands: the host resolves them after
// the canvas's last paint command whatever their place among them, and
// reading the flags first lets the paint ring the hovered estimate. A
// service's tolerance line is declared before its estimates, so a point on
// the line wins.
func (f *frame) declareRegions() (hovered, clicked Point) {
	hovered, clicked = NoPoint, NoPoint
	ids := f.in.Ids
	for range c.IdScope(ids.PrepareStr("tolerances")) {
		for _, m := range f.marks {
			if !m.point.Valid() {
				c.PaintHitRect(ids.PrepareSeq(uint64(m.service)), m.x0, m.y0, m.x1, m.y1).
					Label(m.label).Tooltip(m.tip).Send()
			}
		}
	}
	for range c.IdScope(ids.PrepareStr("estimates")) {
		for s := range f.points {
			for range c.IdScope(ids.PrepareSeq(uint64(s))) {
				for _, m := range f.marks {
					if m.point.Service != s || !m.point.Valid() {
						continue
					}
					fl := c.PaintHitCircle(ids.PrepareSeq(uint64(m.point.Scenario)), m.cx, m.cy, m.r+pointSlack).
						Label(m.label).Tooltip(m.tip).Focusable().SendResp()
					if fl.HasHovered() {
						hovered = m.point
					}
					if fl.HasPrimaryClicked() {
						clicked = m.point
					}
				}
			}
		}
	}
	return
}
