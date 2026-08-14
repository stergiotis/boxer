package storeexec

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Blocked for the same reason chclient is: net/http plus the arrow
// dependency do not build under the WASM targets.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/data/storeexec", PackageProps)
}
