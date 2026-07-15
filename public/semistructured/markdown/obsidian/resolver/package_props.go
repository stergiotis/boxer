package resolver

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
	CapsDirect:       packageprops.Caps(packageprops.CapabilitySafe),
	CapsReachable:    packageprops.Caps(packageprops.CapabilitySafe),
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver", PackageProps)
}
