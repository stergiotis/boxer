package auditrec

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080). The
// wasm states are not asserted: no TinyGo survey has been run over it.
var PackageProps = packageprops.Props{}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit/auditrec", PackageProps)
}
