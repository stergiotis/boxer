package mdnotebook

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Not asserted on any target: the closure reaches the SQL parser and the
// gloss catalog, neither of which is judged.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
	Stability:        packageprops.StabilityExperimental,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook", PackageProps)
}
