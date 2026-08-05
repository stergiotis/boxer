package ignored

import "c"

func trailing() {
	_ = c.NewFrame().Stroke(1.5, c.Hex(0xffffffff)) // designlint:ignore=L10 (legit placeholder)
}

func preceding() {
	// designlint:ignore=L10 (legit placeholder)
	_ = c.NewMapPolyline().Stroke(c.Hex(0xffffffff), 3)
}

func multi() {
	// designlint:ignore=L10,L3 (block intentional)
	_ = c.NewFrame().Stroke(2.5, c.Hex(0xffffffff))
}

func freeFunction() {
	_ = c.PaintEllipseStroke(1.0, 2.0, 3.0, 4.0, c.Hex(0xffffffff), 1.5) // designlint:ignore=L10 (legit placeholder)
}
