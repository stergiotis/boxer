package clean

// No stroke-aware selector is invoked here; nothing the analyzer
// should flag.

type config struct{ Width float32 }

func _() {
	cfg := config{Width: 1.5}
	_ = cfg
	_ = different(2.0)
}

func different(p float32) (v float32) { v = p * 2; return }
