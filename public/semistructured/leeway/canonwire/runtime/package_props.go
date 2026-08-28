package runtime

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Blocked, not amenable: the entity form keys its plains by
// common.PlainItemTypeE (ADR-0210 SD2) and its memberships by
// mappingplan.MembershipChannel (SD4), and both of those packages are
// themselves blocked. Both are leeway vocabulary the wire cannot drop and
// stay lossless. The CBOR writer, the reader and the value forms would
// compile for WASM on their own.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime", PackageProps)
}
