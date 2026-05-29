package violator

import "egui"

var c = egui.C

func insideVertical() {
	for range c.Vertical().KeepIter() {
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() { // want `L5: AllocateUiAtRect inside c\.Vertical`
		}
	}
}

func insideHorizontal() {
	for range c.Horizontal().KeepIter() {
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() { // want `L5: AllocateUiAtRect inside c\.Horizontal`
		}
	}
}

func insideGrid() {
	for range c.Grid("g1").KeepIter() {
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() { // want `L5: AllocateUiAtRect inside c\.Grid`
		}
	}
}

func nestedDeep() {
	for range c.Vertical().KeepIter() {
		for range c.Frame().KeepIter() {
			for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() { // want `L5: AllocateUiAtRect inside c\.Vertical`
			}
		}
	}
}

func sendIterVariant() {
	for range c.Horizontal().SendIter() {
		for range c.AllocateUiAtRect(0, 0, 10, 10).KeepIter() { // want `L5: AllocateUiAtRect inside c\.Horizontal`
		}
	}
}
