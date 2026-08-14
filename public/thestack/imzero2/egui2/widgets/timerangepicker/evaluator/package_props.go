package evaluator

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked transitively, and only transitively: the evaluator resolves range
// expressions through chlocalbroker, which is itself blocked on arrow-go —
// evaluator → keelson/data/chlocalbroker → keelson/runtime/adhocdata →
// arrow/ipc. Nothing here imports arrow directly, so this verdict clears if
// and only if chlocalbroker's does.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/evaluator", PackageProps)
}
