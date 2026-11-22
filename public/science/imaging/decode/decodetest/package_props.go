package decodetest

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
	Kind:             packageprops.KindTestSupport,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/science/imaging/decode/decodetest", PackageProps)
}
