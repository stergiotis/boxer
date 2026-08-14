package persiststore

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Blocked for the same reason the rest of the record-store lane is: the
// generated store pulls in arrow and the ClickHouse executor seam, neither
// of which builds under the WASM targets.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore", PackageProps)
}
