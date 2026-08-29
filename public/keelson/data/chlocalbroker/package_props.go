package chlocalbroker

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// Not asserted on any target: the closure reaches arrow-go, which the survey
// seeded unsupported-external and therefore never probed. Measured, arrow-go
// compiles and runs under TinyGo (ADR-0078, Updates 2026-08-29), the seed is
// gone, and static mode proves only red — so this package stays unjudged
// until a TinyGo that accepts the repo's Go version probes it.
//
// Arrow is intrinsic to what this package is for — brokering clickhouse-local
// means moving Arrow (chlocalbroker → keelson/runtime/adhocdata → arrow/ipc) —
// so its verdict follows arrow-go's rather than an accidental import.
//
// Declared amenable at the 2026-06-12 rollout, which was true then; the
// adhocdata edge came later. It went unnoticed because nothing ran `props
// verify` until it was wired into CI.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/data/chlocalbroker", PackageProps)
}
