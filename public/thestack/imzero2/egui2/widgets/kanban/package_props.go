package kanban

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080). The egui2
// bindings dependency blocks all WASM targets, as for every widget package.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban", PackageProps)
}
