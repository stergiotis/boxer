package ignored

import "c"

func trailing() {
	c.Label("save changes").Send() // designlint:ignore=L1 (legit placeholder)
}

func preceding() {
	// designlint:ignore=L1 (legit placeholder)
	c.Label("open file").Send()
}

func multi() {
	// designlint:ignore=L1,L3 (block intentional)
	c.Label("hello").Send()
}
