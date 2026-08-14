package chlocalbroker

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
//
// Blocked: brokering clickhouse-local means moving Arrow, and the closure
// reaches arrow-go — chlocalbroker → keelson/runtime/adhocdata → arrow/ipc,
// which the survey classifies unsupported-external. Arrow is intrinsic to
// what this package is for, so this is a standing property rather than an
// accidental import worth removing.
//
// Declared amenable at the 2026-06-12 rollout, which was true then; the
// adhocdata edge came later. It went unnoticed because nothing ran `props
// verify` until it was wired into CI.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/keelson/data/chlocalbroker", PackageProps)
}
