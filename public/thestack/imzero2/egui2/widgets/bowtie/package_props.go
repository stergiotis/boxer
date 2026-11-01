package bowtie

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Curated by hand: js and wasip1 checked with the Go toolchain, where the
// layout engine is a stub that reports itself unavailable; the freestanding
// target is unchecked. `boxer code analysis golang wasmsurvey props verify`
// reconciles it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bowtie", PackageProps)
}
