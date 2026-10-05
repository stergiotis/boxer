//go:build wasip1

package tabhost

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// registerReactor makes inst the module's setup: a c-shared module runs no
// main, so the worker's setup export runs the program on the arguments it
// wrote (ADR-0263 SD1).
func registerReactor(inst *Program) {
	browserhost.SetMain(inst.run)
}

// idlFingerprint is the IDL the bindings were generated from; the worker
// compares it with the browser host's before setup and refuses a pair from
// different generations (ADR-0278 SD6, proposed).
//
//go:wasmexport idl_fingerprint
func idlFingerprint() int64 {
	fp := c.IdlFingerprint // through a variable: the conversion wraps, as the worker expects
	return int64(fp)
}
