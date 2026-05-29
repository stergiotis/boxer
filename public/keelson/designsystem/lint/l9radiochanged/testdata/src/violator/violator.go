package violator

import "egui"

var c = egui.C

func chainedHasChanged() {
	var v bool
	if c.RadioButton(1, 0, "a").SendRespVal(&v).HasChanged() { // want `L9: RadioButton\.HasChanged`
	}
}

func deeperChain() {
	var v bool
	if c.RadioButton(2, 0, "b").SendRespVal(&v).HasChanged() { // want `L9: RadioButton\.HasChanged`
		_ = v
	}
}
