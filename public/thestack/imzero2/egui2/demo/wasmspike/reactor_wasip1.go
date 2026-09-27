//go:build wasip1

package main

import "github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"

// The reactor arm (ADR-0263 SD1): a host that built this command with
// -buildmode=c-shared writes the arguments into the buffer browserhost
// exports and calls setup, which runs main's body on them; with -reactor
// the body returns the per-tick step instead of running the loop itself.
func init() {
	browserhost.SetMain(run)
}
