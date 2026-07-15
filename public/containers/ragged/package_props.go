package ragged

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
	CapsDirect:       packageprops.Caps(packageprops.CapabilityRuntime),
	CapsReachable:    packageprops.Caps(packageprops.CapabilityRuntime),
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/containers/ragged", PackageProps)
}
