package assignments

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Blocked on every target because the golden reader takes its rows from a
// natural-key registry ([registry.RegisteredNaturalKey] in SourceI), and
// namemint/registry is itself blocked through leeway/common's arrow
// dependency. The seed said compiles, which was wrong from the start.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments", PackageProps)
}
