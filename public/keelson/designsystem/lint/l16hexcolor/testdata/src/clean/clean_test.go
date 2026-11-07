package clean

import "color"

const testColor uint32 = 0x11223344

func testOnly() {
	_ = color.Hex(0x11223344)
}
