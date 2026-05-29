package violator

import "c"

func raw() {
	c.AddSpace(8.0)                       // want `L3: raw literal 8.0`
	c.AddSpace(12)                        // want `L3: raw literal 12`
	c.AddSpace(2)                         // want `L3: raw literal 2`
}

func chained() {
	f := c.NewFrame()
	_ = f.InnerMargin(8.0)                // want `L3: raw literal 8.0`
	_ = f.OuterMargin(16)                 // want `L3: raw literal 16`
}

func allowlistedHairline() {
	c.AddSpace(0)
	c.AddSpace(0.0)
	c.AddSpace(1)
	c.AddSpace(1.0)
	_ = c.NewFrame().InnerMargin(1.0)
}

// variable arg never triggers — the canonical token-driven form
func tokenForm() {
	v := someToken()
	c.AddSpace(v)
}

func someToken() (v float32) { v = 8.0; return }
