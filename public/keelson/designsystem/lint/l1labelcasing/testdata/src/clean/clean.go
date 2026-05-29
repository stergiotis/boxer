package clean

// No c.Label invocation here — nothing the analyzer should flag.

type cfg struct{ label string }

func _() {
	cfg := cfg{label: "save changes"} // not a c.Label call; ignored
	_ = cfg
	_ = labelHelper("hello")
}

func labelHelper(s string) (out string) { out = s; return }
