package align

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Blocked on every WASM target: AlignAndFormat drives
// golang.org/x/tools/go/packages, which needs a Go toolchain and a real
// filesystem. That is what kept the parent package blocked while these
// files lived in it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/code/synthesis/golang/align", PackageProps)
}
