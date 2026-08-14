package sysmvocab

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Pure registry declarations over the leeway naming/identity packages; no
// syscalls, no I/O.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab", PackageProps)
}
