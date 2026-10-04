package tabhost

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Blocked under TinyGo on both targets through browserhost, whose Serve
// imports net/http/httputil, which TinyGo lacks. The wasip1 tab module is
// built by the Go toolchain, which this does not cover.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/tabhost", PackageProps)
}
