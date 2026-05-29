package ignored

import "color"

func trailing() {
	_ = color.RGB(1, 2, 3) // designlint:ignore=L2 (legit placeholder)
}

func preceding() {
	// designlint:ignore=L2 (legit placeholder)
	_ = color.RGBA(0, 0, 0, 0)
}

func multi() {
	// designlint:ignore=L2,L5 (block intentional)
	_ = color.RGB(0, 0, 0)
}
