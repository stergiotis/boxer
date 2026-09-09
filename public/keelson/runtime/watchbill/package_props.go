package watchbill

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Not asserted on any target, for persiststore's reason: the closure
// reaches arrow-go, which stays unjudged until a TinyGo that accepts the
// repository's Go version probes it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/runtime/watchbill", PackageProps)
}
