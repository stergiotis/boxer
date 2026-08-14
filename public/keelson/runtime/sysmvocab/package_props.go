package sysmvocab

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked: this package's own code is pure declaration, but the WASM verdict
// is over the transitive closure, and the registry it declares into reaches
// arrow-go — sysmvocab → leeway/stopa/registry → leeway/common →
// arrow/array, which the survey classifies unsupported-external.
//
// The first version of this file said "pure registry declarations over the
// leeway naming/identity packages; no syscalls, no I/O" and declared all
// three targets amenable. That reasoning was about this package's own code
// and was correct about it — the verdict it produced was still wrong,
// because no reader computes a transitive closure by inspection. Which is
// what the survey is for, and why `props verify` gates it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab", PackageProps)
}
