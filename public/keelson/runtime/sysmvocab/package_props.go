package sysmvocab

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Not asserted on any target: the closure reaches arrow-go, which the survey
// seeded unsupported-external and therefore never probed. Measured, arrow-go
// compiles and runs under TinyGo (ADR-0078, Updates 2026-08-29), the seed is
// gone, and static mode proves only red — so this package stays unjudged
// until a TinyGo that accepts the repo's Go version probes it.
//
// This package's own code is pure declaration, but the verdict is over the
// transitive closure: sysmvocab → leeway/namemint/registry → leeway/common →
// arrow/array.
//
// The first version of this file said "pure registry declarations over the
// leeway naming/identity packages; no syscalls, no I/O" and declared all
// three targets amenable. That reasoning was about this package's own code
// and was correct about it — the verdict it produced was still unfounded,
// because no reader computes a transitive closure by inspection. Which is
// what the survey is for, and why `props verify` gates it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab", PackageProps)
}
