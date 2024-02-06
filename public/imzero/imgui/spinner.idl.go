//go:build fffi_idl_code

package imgui

func SpinnerDots(label string, nextdotP float32, radius float32, thickness float32) (nextdot float32) {
	_ = `ImSpinner::SpinnerDots(label, &nextdotP, radius, thickness);
nextdot = nextdotP;`
	return
}
func SpinnerDotsV(label string, nextdotP float32, radius float32, thickness float32, color uint32, speed float32, dots Size_t, minth float32) (nextdot float32) {
	_ = `ImSpinner::SpinnerDots(label, &nextdotP, radius, thickness, color, speed, dots, minth);
nextdot = nextdotP;`
	return
}

func SpinnerDemos() {
	_ = `ImSpinner::demoSpinners()`
}
