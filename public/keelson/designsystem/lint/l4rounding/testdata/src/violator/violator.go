package violator

import "c"

func raw() {
	_ = c.NewFrame().CornerRadius(4)         // want `L4: raw literal 4`
	_ = c.NewFrame().CornerRadius(6.0)       // want `L4: raw literal 6.0`
	_ = c.NewProgressBar().CornerRadius(2)   // want `L4: raw literal 2`
	_ = c.NewTintedScope().CornerRadius(3.0) // want `L4: raw literal 3.0`
}

func offLadder() {
	_ = c.NewFrame().CornerRadius(3) // want `L4: raw literal 3`
	_ = c.NewFrame().CornerRadius(5) // want `L4: raw literal 5`
}

func allowlistedSharp() {
	_ = c.NewFrame().CornerRadius(0)
	_ = c.NewFrame().CornerRadius(0.0)
}

// variable arg never triggers — the canonical token-driven form
func tokenForm() {
	r := someRounding()
	_ = c.NewFrame().CornerRadius(r)
}

func someRounding() (v float32) { v = 4.0; return }
