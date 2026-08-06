package codevol

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate`; curate by hand, then `wasmsurvey props verify`.
//
// Blocked on every WASM target: ReadSelfSymbols reads the running
// executable's own ELF symbol table, which none of the WASM targets provide.
// The other two tiers would compile.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/code/analysis/golang/codevol", PackageProps)
}
