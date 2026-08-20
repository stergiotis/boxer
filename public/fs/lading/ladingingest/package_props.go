package ladingingest

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked for the reason every leeway-facing package is: the WASM verdict is
// over the transitive closure, and this one reaches arrow-go through the
// generated stores it writes to, which the survey classifies
// unsupported-external.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingingest", PackageProps)
}
