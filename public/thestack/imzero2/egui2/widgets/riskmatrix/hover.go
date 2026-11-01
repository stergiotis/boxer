package riskmatrix

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// mark is a hit region declared over what it covers: a risk dot (a circle)
// or a banded cell (a rect). label is its accessible name and the tooltip's
// first line, tip the tooltip's body. risk is the risk's index, -1 for a
// cell.
type mark struct {
	risk           int
	circle         bool
	cx, cy, r      float32
	x0, y0, x1, y1 float32
	label, tip     string
}

// dotSlack widens a dot's region past its painted radius, so a small dot is
// easy to point at.
const dotSlack float32 = 2

// declareRegions emits the frame's hit regions (ADR-0149's hit regions) and
// returns, from their flags of the previous frame, the risk hovered and the
// one clicked, -1 for none. The host hit-tests them, shows the winner's
// tooltip at the pointer and gives each region an accessibility node named by
// its label; the risk dots are also Tab stops, and Enter or Space on a
// focused dot comes back as its click.
//
// The regions go out before the paint commands: the host resolves them after
// the canvas's last paint command whatever their place among them, and
// reading the flags first lets the paint highlight the hovered dot. Cells are
// declared before dots, so a dot wins over the cell under it.
func (f *frame) declareRegions() (hovered, clicked int) {
	hovered, clicked = -1, -1
	ids := f.in.Ids
	for range c.IdScope(ids.PrepareStr("cells")) {
		n := 0
		for _, m := range f.marks {
			if m.risk >= 0 {
				continue
			}
			c.PaintHitRect(ids.PrepareSeq(uint64(n)), m.x0, m.y0, m.x1, m.y1).
				Label(m.label).Tooltip(m.tip).Send()
			n++
		}
	}
	for range c.IdScope(ids.PrepareStr("risks")) {
		for _, m := range f.marks {
			if m.risk < 0 {
				continue
			}
			fl := c.PaintHitCircle(ids.PrepareSeq(uint64(m.risk)), m.cx, m.cy, m.r+dotSlack).
				Label(m.label).Tooltip(m.tip).Focusable().SendResp()
			if fl.HasHovered() {
				hovered = m.risk
			}
			if fl.HasPrimaryClicked() {
				clicked = m.risk
			}
		}
	}
	return
}
