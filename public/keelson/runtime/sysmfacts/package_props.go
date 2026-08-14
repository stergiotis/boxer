package sysmfacts

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Blocked: the generated store composes the arrow and ClickHouse-executor
// stack, which does not build under the WASM targets.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts", PackageProps)
}
