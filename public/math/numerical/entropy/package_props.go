package entropy

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate`; curate by hand, then `wasmsurvey props verify`.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/math/numerical/entropy", PackageProps)
}
