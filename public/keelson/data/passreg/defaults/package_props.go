package defaults

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
	CapsDirect:       packageprops.Caps(packageprops.CapabilitySafe),
	CapsReachable:    packageprops.Caps(packageprops.CapabilityArbitraryExecution, packageprops.CapabilityReflect),
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/data/passreg/defaults", PackageProps)
}
