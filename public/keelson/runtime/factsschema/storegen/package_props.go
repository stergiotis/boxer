package storegen

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Blocked: this is a generation-time package, reached only from a gen test,
// and it pulls in the recordstore generator's own toolchain dependencies.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen", PackageProps)
}
