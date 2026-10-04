//go:build wasip1

package tabhost

import "github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"

// registerReactor makes inst the module's setup: a c-shared module runs no
// main, so the worker's setup export runs the program on the arguments it
// wrote (ADR-0263 SD1).
func registerReactor(inst *Program) {
	browserhost.SetMain(inst.run)
}
