package runtime

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
	CapsDirect:       packageprops.Caps(packageprops.CapabilitySafe),
	CapsReachable:    packageprops.Caps(packageprops.CapabilityReadSystemState, packageprops.CapabilityArbitraryExecution, packageprops.CapabilityUnsafePointer, packageprops.CapabilityReflect),
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime", PackageProps)
}
