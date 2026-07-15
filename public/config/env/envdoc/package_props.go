package envdoc

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
	CapsDirect:       packageprops.Caps(packageprops.CapabilityFiles),
	CapsReachable:    packageprops.Caps(packageprops.CapabilityFiles, packageprops.CapabilityArbitraryExecution, packageprops.CapabilityUnsafePointer, packageprops.CapabilityReflect),
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/config/env/envdoc", PackageProps)
}
