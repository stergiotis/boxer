package ignored

import "egui"

var c = egui.C

func legitWorkaround() {
	var v bool
	// designlint:ignore=L9 (we know; testing the click-detection bypass)
	if c.RadioButton(1, 0, "a").SendRespVal(&v).HasChanged() {
	}
}
