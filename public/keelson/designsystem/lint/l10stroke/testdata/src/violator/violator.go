package violator

import "c"

func widthFirst() {
	_ = c.NewFrame().Stroke(1.5, c.Hex(0xffffffff))     // want `L10: raw literal 1.5`
	_ = c.NewFrame().Stroke(2, c.Hex(0xffffffff))       // want `L10: raw literal 2`
	_ = c.NewTintedScope().Stroke(1, c.Hex(0xff00ffff)) // want `L10: raw literal 1`
}

func colorFirst() {
	_ = c.NewH3Region().Stroke(c.Hex(0x123456ff), 1.5)  // want `L10: raw literal 1.5`
	_ = c.NewMapPolyline().Stroke(c.Hex(0xabcdef00), 3) // want `L10: raw literal 3`
}

func offLadder() {
	_ = c.NewFrame().Stroke(2.5, c.Hex(0xffffffff)) // want `L10: raw literal 2.5`
	_ = c.NewFrame().Stroke(0.8, c.Hex(0xffffffff)) // want `L10: raw literal 0.8`
}

func allowlistedNone() {
	_ = c.NewFrame().Stroke(0, c.Hex(0xffffffff))
	_ = c.NewFrame().Stroke(0.0, c.Hex(0xffffffff))
	_ = c.NewMapPolyline().Stroke(c.Hex(0xffffffff), 0)
}

// variable arg never triggers — the canonical token-driven form
func tokenForm() {
	w := someStroke()
	_ = c.NewFrame().Stroke(w, c.Hex(0xffffffff))
	_ = c.NewMapPolyline().Stroke(c.Hex(0xffffffff), w)
}

// free painter functions: only the registered per-name width position
// triggers — the coordinate (and rect rounding) args are bare numeric
// literals too and must never be flagged.
func freeFunctionWidth() {
	_ = c.PaintEllipseStroke(300.0, 245.0, 55.0, 30.0, c.Hex(0xdd99ffff), 1.5) // want `L10: raw literal 1.5`
	_ = c.PaintEllipseStroke(410.0, 245.0, 30.0, 42.0, c.Hex(0x44ddffff), 2)   // want `L10: raw literal 2`
	_ = c.PaintCircleStroke(120.0, 80.0, 24.0, c.Hex(0xffffffff), 1.5)         // want `L10: raw literal 1.5`
	_ = c.PaintRectStroke(10.0, 10.0, 90.0, 60.0, 4.0, c.Hex(0xffffffff), 2)   // want `L10: raw literal 2`
}

func freeFunctionAllowed() {
	_ = c.PaintEllipseStroke(80.0, 100.0, 48.0, 48.0, c.Hex(0xffffffff), 0) // sentinel "no stroke"
	w := someStroke()
	_ = c.PaintEllipseStroke(80.0, 100.0, 48.0, 48.0, c.Hex(0xffffffff), w)    // token-driven form
	_ = c.PaintCircleStroke(120.0, 80.0, 24.0, c.Hex(0xffffffff), w)           // token-driven form
	_ = c.PaintRectStroke(10.0, 10.0, 90.0, 60.0, 4.0, c.Hex(0xffffffff), w)   // rounding literal is L4's concern, not L10's
	_ = c.PaintRectStroke(10.0, 10.0, 90.0, 60.0, 4.0, c.Hex(0xffffffff), 0.0) // sentinel "no stroke"
}

func someStroke() (v float32) { v = 1.5; return }
