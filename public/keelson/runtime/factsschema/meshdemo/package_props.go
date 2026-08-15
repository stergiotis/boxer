package meshdemo

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080): the demo
// binds the recordstore stack, which does not build under the WASM targets.
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/factsschema/meshdemo", PackageProps)
}
