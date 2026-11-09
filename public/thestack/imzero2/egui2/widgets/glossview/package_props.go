package glossview

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Not asserted on any target: the closure reaches the gloss catalog, which is
// unjudged itself (see its PackageProps).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/glossview", PackageProps)
}
