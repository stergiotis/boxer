package ladingsql

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked: the expansion reads the generated stores' published artefacts, so
// the transitive closure reaches arrow-go, which the survey classifies
// unsupported-external.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/fs/lading/ladingsql", PackageProps)
}
