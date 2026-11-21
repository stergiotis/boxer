package ops

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
	Stability:        packageprops.StabilityExperimental,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/science/imaging/ops", PackageProps)
}
