package extbin

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate`; curate by hand, then `wasmsurvey props verify`.
// os/exec compiles for every WASM target (it fails only at run time); the
// package pulls in no cgo or blocked dependencies.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/extbin", PackageProps)
}
