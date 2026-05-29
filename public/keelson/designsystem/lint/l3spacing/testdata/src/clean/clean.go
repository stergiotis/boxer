package clean

// No spacing-aware selector is invoked here; nothing the analyzer
// should flag.

type config struct{ Padding float32 }

func _() {
	cfg := config{Padding: 8.0}
	_ = cfg
	_ = different(8.0)
}

func different(p float32) (v float32) { v = p * 2; return }
