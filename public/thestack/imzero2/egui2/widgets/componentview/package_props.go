package componentview

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked: rendering leeway components means reading them reflectively, and
// the reflective marshaller's closure reaches arrow-go — componentview →
// leeway/marshall/go/marshallreflect → arrow/array, which the survey
// classifies unsupported-external. The dependency is what the widget is for.
//
// Declared amenable at the 2026-06-12 rollout, which was true then; the
// marshallreflect edge came later and nothing was checking.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview", PackageProps)
}
