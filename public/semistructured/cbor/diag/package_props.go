package diag

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Pure Go over bytes and strings — the same closure the parent cbor package
// compiles under, so it carries the parent's verdict.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/cbor/diag", PackageProps)
}
