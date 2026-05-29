package violator

import "c"

func boolForms() {
	var out float64
	c.AnimateBoolWithTime(1, true, 0.4)              // want `L11: raw literal 0.4`
	c.AnimateBoolWithTimeBind(2, true, 0.16, &out)   // want `L11: raw literal 0.16`
	c.AnimateBoolWithTimeBind(3, true, 1, &out)      // want `L11: raw literal 1`
}

func valueForms() {
	var out float64
	c.AnimateValueWithTime(4, 1.0, 0.32)               // want `L11: raw literal 0.32`
	c.AnimateValueWithTimeBind(5, 1.0, 0.6, &out)      // want `L11: raw literal 0.6`
}

func allowlistedZero() {
	var out float64
	c.AnimateBoolWithTimeBind(6, true, 0, &out)
	c.AnimateBoolWithTimeBind(7, true, 0.0, &out)
	c.AnimateValueWithTime(8, 1.0, 0)
}

func responsiveNeverFires() {
	// AnimateBoolResponsive has no durSecs — must never trigger even
	// though its name shares the "Animate" prefix.
	c.AnimateBoolResponsive(9, true)
}

// variable arg never triggers — the canonical token-driven form
func tokenForm() {
	d := someDuration()
	var out float64
	c.AnimateBoolWithTimeBind(10, true, d, &out)
	c.AnimateValueWithTime(11, 1.0, d)
}

func someDuration() (s float32) { s = 0.16; return }
