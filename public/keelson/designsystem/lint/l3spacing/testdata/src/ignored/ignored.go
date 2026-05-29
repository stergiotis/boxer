package ignored

import "c"

func trailing() {
	c.AddSpace(8.0) // designlint:ignore=L3 (legit placeholder)
}

func preceding() {
	// designlint:ignore=L3 (legit placeholder)
	c.AddSpace(12)
}

func multi() {
	// designlint:ignore=L3,L5 (block intentional)
	_ = c.NewFrame().InnerMargin(16)
}
