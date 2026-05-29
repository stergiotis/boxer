package clean

// No rounding-aware selector is invoked here; nothing the analyzer
// should flag.

type config struct{ Radius float32 }

func _() {
	cfg := config{Radius: 4.0}
	_ = cfg
	_ = different(4.0)
}

func different(p float32) (v float32) { v = p * 2; return }
