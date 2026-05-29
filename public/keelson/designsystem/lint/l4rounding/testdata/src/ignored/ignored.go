package ignored

import "c"

func trailing() {
	_ = c.NewFrame().CornerRadius(4) // designlint:ignore=L4 (legit placeholder)
}

func preceding() {
	// designlint:ignore=L4 (legit placeholder)
	_ = c.NewFrame().CornerRadius(6)
}

func multi() {
	// designlint:ignore=L4,L3 (block intentional)
	_ = c.NewFrame().CornerRadius(8)
}
