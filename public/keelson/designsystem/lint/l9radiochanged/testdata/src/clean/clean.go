package clean

import "egui"

var c = egui.C

func checkboxHasChangedIsFine() {
	// Checkbox.HasChanged is correct — only RadioButton trips the lint.
	var v bool
	if c.Checkbox(1, false, "a").SendRespVal(&v).HasChanged() {
	}
}

func radioPrimaryClickedIsFine() {
	var v bool
	if c.RadioButton(1, 0, "a").SendRespVal(&v).HasPrimaryClicked() {
	}
}

func brokenChainNotDetected() {
	// v1 limitation: variable-broken chains aren't detected. The lint
	// docstring documents this as a known false-negative.
	var v bool
	resp := c.RadioButton(1, 0, "a").SendRespVal(&v)
	if resp.HasChanged() {
	}
}
