// Package axisruler paints a linear tick axis — a baseline, tick marks, and
// text labels — along one side of a rectangular plot area. It is the shared
// renderer behind the timeline's calendar axis and the spectrumdisplay's
// frequency / power / time gutters: the caller computes tick positions and
// labels however it likes (finddivisions, timeticks, …) and hands them here as
// pixel coordinates; this package owns only the visual treatment — theme colors
// via [styletokens], tick length, label gap, and edge-aware label anchoring so
// the first and last labels do not clip the rect.
//
// It emits Paint* opcodes into the caller's *current* canvas coordinate space;
// it does not allocate a Ui or a PaintCanvas — the caller owns the canvas
// lifecycle and its background. So the coordinates handed in are whatever the
// caller's canvas uses: rect-local for a per-gutter canvas (spectrumdisplay),
// or canvas-absolute for one big canvas (timeline). The pure label-placement
// math ([labelAnchors]) carries no GUI and is unit-tested directly.
//
//	st := axisruler.DefaultStyle()
//	axisruler.Paint(axisruler.SideBottom, baselineY, x0, x1, ticks, st) // X axis
//	axisruler.Paint(axisruler.SideLeft, gutterRightX, y0, y1, ticks, st) // Y axis
//
// Not goroutine-safe; drive from the UI goroutine (the painter discipline).
package axisruler

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// SideE selects which edge of the plot area the axis sits on. It fixes the
// tick-mark direction and the resting label anchor; the cross-side anchor then
// flips inward at the two ends when [Style.EdgeAnchor] is set.
type SideE uint8

const (
	// SideBottom is a horizontal axis below the data: a baseline spanning the
	// x range, tick marks pointing down, labels below the ticks (top-anchored,
	// centered — left/right at the two ends).
	SideBottom SideE = iota
	// SideLeft is a vertical axis left of the data: a baseline spanning the y
	// range, tick marks pointing left, labels left of the ticks (right-anchored,
	// vertically centered — top/bottom at the two ends).
	SideLeft
)

// Tick is one tick mark: a pixel position along the axis (x for [SideBottom],
// y for [SideLeft], in the caller's canvas coordinates) and its label.
type Tick struct {
	Pos   float32
	Label string
}

// Style is the axis visual treatment. Start from [DefaultStyle] and tweak.
// AxisColor and TickColor are separate so a caller can tint the baseline and the
// tick marks differently (the timeline exposes both); [DefaultStyle] sets them
// equal.
type Style struct {
	AxisColor   color.Color // baseline
	TickColor   color.Color // tick marks
	LabelColor  color.Color // tick labels
	FontSize    float32     // label font size, logical pixels
	TickLen     float32     // tick-mark length, pixels
	LabelGap    float32     // gap between a tick mark and its label, pixels
	StrokeWidth float32     // baseline / tick-mark stroke width, pixels
	EdgePad     float32     // a tick within EdgePad of an end anchors its label inward
	Baseline    bool        // draw the axis baseline across [lo,hi]
	EdgeAnchor  bool        // anchor the first/last label inward so it does not clip
}

// DefaultStyle is the IDS-token-derived treatment: a faint border for the
// baseline and ticks ([styletokens.NeutralBorderFaint]) and secondary text for
// the labels ([styletokens.NeutralTextSecondary]) — the same tokens the
// timeline axis uses, so rulers across widgets read as one fleet.
func DefaultStyle() (st Style) {
	axis := color.Hex(styletokens.NeutralBorderFaint.AsHex()).Keep()
	st = Style{
		AxisColor:   axis,
		TickColor:   axis,
		LabelColor:  color.Hex(styletokens.NeutralTextSecondary.AsHex()).Keep(),
		FontSize:    11,
		TickLen:     4,
		LabelGap:    3,
		StrokeWidth: 1.0,
		EdgePad:     12,
		Baseline:    true,
		EdgeAnchor:  true,
	}
	return
}

// Anchor codes for PaintText (anchorH: 0=left 1=center 2=right; anchorV: 0=top
// 1=center 2=bottom) — the egui2 painter convention.
const (
	anchorLeft    uint8 = 0
	anchorCenter  uint8 = 1
	anchorRight   uint8 = 2
	anchorTop     uint8 = 0
	anchorVCenter uint8 = 1
	anchorBottom  uint8 = 2
)

// Paint emits the baseline, tick marks, and labels for ticks along side.
//
// base is the cross-axis pixel coordinate of the axis line — the gutter edge
// adjacent to the data: for [SideBottom] it is the baseline Y (ticks and labels
// go below it); for [SideLeft] it is the right-edge X (ticks and labels go to
// its left). lo and hi are the along-axis pixel bounds (x for [SideBottom], y
// for [SideLeft]): the baseline spans them and end labels anchor inward within
// them. All coordinates are in the caller's current canvas space. Ticks outside
// [lo,hi] are still drawn — the caller clips by choosing what to pass.
func Paint(side SideE, base, lo, hi float32, ticks []Tick, st Style) {
	switch side {
	case SideBottom:
		if st.Baseline {
			c.PaintLine(lo, base, hi, base, st.AxisColor, st.StrokeWidth).Send()
		}
		for _, t := range ticks {
			c.PaintLine(t.Pos, base, t.Pos, base+st.TickLen, st.TickColor, st.StrokeWidth).Send()
			h, v := labelAnchors(side, t.Pos, lo, hi, st)
			c.PaintText(t.Pos, base+st.TickLen+st.LabelGap, h, v, t.Label, st.FontSize, st.LabelColor).Send()
		}
	case SideLeft:
		if st.Baseline {
			c.PaintLine(base, lo, base, hi, st.AxisColor, st.StrokeWidth).Send()
		}
		for _, t := range ticks {
			c.PaintLine(base-st.TickLen, t.Pos, base, t.Pos, st.TickColor, st.StrokeWidth).Send()
			h, v := labelAnchors(side, t.Pos, lo, hi, st)
			c.PaintText(base-st.TickLen-st.LabelGap, t.Pos, h, v, t.Label, st.FontSize, st.LabelColor).Send()
		}
	}
}

// labelAnchors returns the (horizontal, vertical) PaintText anchor for a label
// at along-axis position pos within [lo,hi]. The resting anchor keeps the label
// clear of its tick (below-and-centered for [SideBottom], left-and-centered for
// [SideLeft]); with [Style.EdgeAnchor], a tick within [Style.EdgePad] of either
// end flips the *along-axis* anchor inward (left/right at the bottom axis, top/
// bottom at the left axis) so the label stays inside [lo,hi] instead of
// overhanging the rect edge. Pure — no GUI — so it is unit-tested directly.
func labelAnchors(side SideE, pos, lo, hi float32, st Style) (h, v uint8) {
	switch side {
	case SideBottom:
		h, v = anchorCenter, anchorTop
		if st.EdgeAnchor {
			switch {
			case pos-lo < st.EdgePad:
				h = anchorLeft
			case hi-pos < st.EdgePad:
				h = anchorRight
			}
		}
	case SideLeft:
		h, v = anchorRight, anchorVCenter
		if st.EdgeAnchor {
			switch {
			case pos-lo < st.EdgePad:
				v = anchorTop
			case hi-pos < st.EdgePad:
				v = anchorBottom
			}
		}
	}
	return
}
