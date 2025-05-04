//go:build !bootstrap

package imgui

import "github.com/stergiotis/boxer/public/logical"

// TableNextColumnS Caches column visibility to prevent fffi roundtrip in native TableNextColumn.
func TableNextColumnS(isVisibleStore *logical.Tristate) (visible bool) {
	if isVisibleStore.IsNil() {
		if TableNextColumn() {
			*isVisibleStore = logical.TriTrue
			visible = true
		} else {
			*isVisibleStore = logical.TriFalse
		}
	} else {
		TableNextColumnP()
		visible = isVisibleStore.IsTrue()
	}
	return
}
