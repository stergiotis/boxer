package clean

// No color package import; nothing the analyzer should flag.

type rgbaStruct struct{ R, G, B, A uint8 }

func _() {
	_ = rgbaStruct{1, 2, 3, 4}
	_ = different(1, 2, 3)
}

func different(r, g, b uint8) (v uint8) { v = r ^ g ^ b; return }
