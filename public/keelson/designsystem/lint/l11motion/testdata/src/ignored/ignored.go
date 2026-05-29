package ignored

import "c"

func trailing() {
	var out float64
	c.AnimateBoolWithTimeBind(1, true, 0.4, &out) // designlint:ignore=L11 (legit placeholder)
}

func preceding() {
	var out float64
	// designlint:ignore=L11 (legit placeholder)
	c.AnimateValueWithTimeBind(2, 1.0, 0.6, &out)
}

func multi() {
	// designlint:ignore=L11,L3 (block intentional)
	c.AnimateBoolWithTime(3, true, 0.28)
}
