package keycodes

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080). A table of
// constants with no imports beyond this registration — it compiles anywhere,
// unlike the bindings and widget packages that consume it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes", PackageProps)
}
