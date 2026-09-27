//go:build wasip1

package main

import "github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"

// A c-shared module runs no main: the reactor's setup export runs this
// binary's body on the arguments the worker wrote (ADR-0263 SD1).
func init() {
	browserhost.SetMain(run)
}
