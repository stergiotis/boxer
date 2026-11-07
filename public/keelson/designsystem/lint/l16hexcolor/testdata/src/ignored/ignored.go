package ignored

import "color"

const bandColor uint32 = 0x4c9f7038 // designlint:ignore=L16 (fixture placeholder)

func trailing() {
	_ = color.Hex(0x44ff44ff) // designlint:ignore=L16 (fixture placeholder)
}

func preceding() {
	// designlint:ignore=L16 (fixture placeholder)
	_ = color.Hex(0xffffffff)
}

func multi() {
	// designlint:ignore=L2,L16 (block intentional)
	_ = color.Hex(0x202020ff)
}
