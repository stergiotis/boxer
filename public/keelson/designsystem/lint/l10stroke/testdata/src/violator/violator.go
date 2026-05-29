package violator

import "c"

func widthFirst() {
	_ = c.NewFrame().Stroke(1.5, c.Hex(0xffffffff))    // want `L10: raw literal 1.5`
	_ = c.NewFrame().Stroke(2, c.Hex(0xffffffff))      // want `L10: raw literal 2`
	_ = c.NewTintedScope().Stroke(1, c.Hex(0xff00ffff)) // want `L10: raw literal 1`
}

func colorFirst() {
	_ = c.NewH3Region().Stroke(c.Hex(0x123456ff), 1.5)        // want `L10: raw literal 1.5`
	_ = c.NewMapPolyline().Stroke(c.Hex(0xabcdef00), 3)       // want `L10: raw literal 3`
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

func someStroke() (v float32) { v = 1.5; return }
