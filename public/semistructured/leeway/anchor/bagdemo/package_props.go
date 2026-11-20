package bagdemo

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
	Kind:             packageprops.KindDemo,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/anchor/bagdemo", PackageProps)
}
