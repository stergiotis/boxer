package loadstudy

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// The WASM fields are deliberately left unset. This package speaks HTTP to a
// ClickHouse server, so no wasm target is a plausible destination for it, and
// asserting Blocked without having run the survey would be a claim rather than a
// verdict. The zero value asserts nothing, which is the honest state.
var PackageProps = packageprops.Props{
	Kind: packageprops.KindIntegrationTest,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/analytics/timeseries/loadstudy", PackageProps)
}
