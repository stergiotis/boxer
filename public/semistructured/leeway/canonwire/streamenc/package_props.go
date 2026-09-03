package streamenc

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by hand from the parent package's entry; curate by hand. The same
// group's `props verify` reconciles it.
//
// Not asserted on any target, as canonwire and canonform are not: the closure
// reaches arrow-go, which the survey never probed on a browser target.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/streamenc", PackageProps)
}
