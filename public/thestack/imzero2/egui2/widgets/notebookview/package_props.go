package notebookview

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Not asserted on any target: the closure reaches mdnotebook and the
// markdown widget, neither of which is judged.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview", PackageProps)
}
