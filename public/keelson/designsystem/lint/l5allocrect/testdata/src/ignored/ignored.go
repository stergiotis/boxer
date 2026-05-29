package ignored

import "egui"

var c = egui.C

func overlayTooltip() {
	for range c.Vertical().KeepIter() {
		// designlint:ignore=L5 (tooltip overlay; absolute placement is intentional)
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() {
		}
	}
}

func trailingIgnore() {
	for range c.Horizontal().KeepIter() {
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() { // designlint:ignore=L5 (custom paint region)
		}
	}
}
