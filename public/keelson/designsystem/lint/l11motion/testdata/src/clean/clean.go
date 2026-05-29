package clean

// No motion-aware selector is invoked here; nothing the analyzer
// should flag.

type config struct{ DurSecs float32 }

func _() {
	cfg := config{DurSecs: 0.4}
	_ = cfg
	_ = different(0.16)
}

func different(p float32) (v float32) { v = p * 2; return }
