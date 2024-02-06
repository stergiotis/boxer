package imgui

const ImguiUsesBGRAColorFormat = true

func ColorU32(rgba uint32) (c uint32) {
	//return bits.Reverse32(rgba)
	return Color32U8(uint8(rgba>>24),
		uint8(rgba>>16),
		uint8(rgba>>8),
		uint8(rgba),
	)
}
func ColorU32ToImVec(rgba uint32) (c ImVec4) {
	r := uint8(rgba)
	g := uint8(rgba >> 8)
	b := uint8(rgba >> 16)
	a := uint8(rgba >> 24)
	if ImguiUsesBGRAColorFormat {
		return ImVec4([4]float32{float32(b) / 255.0, float32(g) / 255.0, float32(r) / 255.0, float32(a) / 255.0})
	} else {
		return ImVec4([4]float32{float32(r) / 255.0, float32(g) / 255.0, float32(b) / 255.0, float32(a) / 255.0})
	}
}
func Color32U8(r uint8, g uint8, b uint8, a uint8) (c uint32) {
	if ImguiUsesBGRAColorFormat {
		c = uint32(a) << 24
		c = c | uint32(r)<<16
		c = c | uint32(g)<<8
		c = c | uint32(b)
	} else {
		c = uint32(a) << 24
		c = c | uint32(b)<<16
		c = c | uint32(g)<<8
		c = c | uint32(r)
	}
	return
}

// ToColorU32 see ColorConvertFloat4ToU32
func (inst ImVec4) ToColorU32() uint32 {
	var r, g, b, a float32
	if ImguiUsesBGRAColorFormat {
		b = inst[0]
		g = inst[1]
		r = inst[2]
		a = inst[3]
	} else {
		r = inst[0]
		g = inst[1]
		b = inst[2]
		a = inst[3]
	}
	if r < 0.0 {
		r = 0.0
	}
	if r > 1.0 {
		r = 1.0
	}
	if g < 0.0 {
		g = 0.0
	}
	if g > 1.0 {
		g = 1.0
	}
	if b < 0.0 {
		b = 0.0
	}
	if b > 1.0 {
		b = 1.0
	}
	if a < 0.0 {
		a = 0.0
	}
	if a > 1.0 {
		a = 1.0
	}
	return Color32U8(
		uint8(r*255.0+0.5),
		uint8(g*255.0+0.5),
		uint8(b*255.0+0.5),
		uint8(a*255.0+0.5),
	)
}
