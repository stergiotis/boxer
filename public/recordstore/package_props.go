package recordstore

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Blocked like dml: the arrow dependency does not compile under TinyGo.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() { packageprops.Register("github.com/stergiotis/boxer/public/recordstore", PackageProps) }
