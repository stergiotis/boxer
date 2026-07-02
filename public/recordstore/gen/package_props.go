package gen

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Blocked like recordstore: the leeway generators it drives pull in the
// arrow dependency, which does not compile under TinyGo.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/recordstore/gen", PackageProps)
}
