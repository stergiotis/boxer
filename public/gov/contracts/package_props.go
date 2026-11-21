package contracts

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
// A repository tool: it walks the checkout and links the imzero2 bindings.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
	Kind:             packageprops.KindTool,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/gov/contracts", PackageProps)
}
