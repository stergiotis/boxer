package glosssql

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked through the catalog it validates against (public/hmi/gloss reads
// Arrow arrays; arrow-go is unsupported-external in the survey).
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/hmi/gloss/glosssql", PackageProps)
}
