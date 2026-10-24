package mdblocks

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps declares the package's build posture (ADR-0080).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdblocks", PackageProps)
}
