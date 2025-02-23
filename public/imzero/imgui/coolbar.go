//go:build !bootstrap

package imgui

import "github.com/stergiotis/boxer/public/fffi/runtime"

type ImCoolBarFlags int

const (
	ImCoolBarFlags_None       ImCoolBarFlags = 0
	ImCoolBarFlags_Vertical   ImCoolBarFlags = 1 << 0
	ImCoolBarFlags_Horizontal ImCoolBarFlags = 1 << 1
)

type ImCoolBarConfigForeignPtr uintptr

func (foreignptr ImCoolBarConfigForeignPtr) getFffi() *runtime.Fffi2 {
	return currentFffiVar
}

func (foreignptr ImCoolBarConfigForeignPtr) handleError(err error) {
	currentFffiErrorHandler(err)
}
