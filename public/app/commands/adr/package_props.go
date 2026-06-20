package adr

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// The command walks the filesystem and shells out to clickhouse-local, so it
// is not WASM-amenable on any target.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/app/commands/adr", PackageProps)
}
