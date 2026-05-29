package violator

import "color"

func raw() {
	_ = color.RGB(1, 2, 3)         // want `L2: raw color\.RGB`
	_ = color.RGBA(1, 2, 3, 4)     // want `L2: raw color\.RGBA`
	_ = color.RGBA(0, 0, 0, 0)     // want `L2: raw color\.RGBA`
}

func notTriggered() {
	_ = color.Other(1, 2, 3) // not a triggering selector
}
