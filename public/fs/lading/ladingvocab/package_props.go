package ladingvocab

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked for the same reason sysmvocab is: the code here is pure
// declaration, but the WASM verdict is over the transitive closure, and the
// registry it declares into reaches arrow-go — ladingvocab →
// leeway/namemint/registry → leeway/common → arrow/array, which the survey
// classifies unsupported-external.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingvocab", PackageProps)
}
