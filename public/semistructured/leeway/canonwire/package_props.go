package canonwire

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Blocked, not amenable: the generator reads a common.TableDesc and speaks
// mappingplan.MembershipChannel, and both of those packages are themselves
// blocked. It is generation-time code and has no reason to run in a browser.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/canonwire", PackageProps)
}
