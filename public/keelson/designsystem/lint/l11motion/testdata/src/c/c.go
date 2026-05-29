// Stand-in for the egui2 animation surface used by analysistest
// fixtures. L11 detection is syntactic on the selector name and the
// per-name durSecs arg index; full import paths are irrelevant.
package c

func AnimateBoolWithTime(animId uint64, target bool, durSecs float32) {
	_ = animId
	_ = target
	_ = durSecs
}

func AnimateBoolWithTimeBind(animId uint64, target bool, durSecs float32, out *float64) {
	_ = animId
	_ = target
	_ = durSecs
	_ = out
}

func AnimateValueWithTime(animId uint64, target float32, durSecs float32) {
	_ = animId
	_ = target
	_ = durSecs
}

func AnimateValueWithTimeBind(animId uint64, target float32, durSecs float32, out *float64) {
	_ = animId
	_ = target
	_ = durSecs
	_ = out
}

// AnimateBoolResponsive has no durSecs — verifies the analyzer doesn't
// pick it up by accident.
func AnimateBoolResponsive(animId uint64, target bool) { _ = animId; _ = target }
