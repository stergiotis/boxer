package ladingadhoc

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Not asserted on any target: the closure reaches arrow-go through the lading
// stores, which the survey seeded unsupported-external and therefore never
// probed — the sibling lading packages carry the same note.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingadhoc", PackageProps)
}
