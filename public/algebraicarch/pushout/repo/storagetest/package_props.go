package storagetest

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMCompiles,
	WASMJS:           packageprops.WASMCompiles,
	WASMFreestanding: packageprops.WASMCompiles,
	Kind:             packageprops.KindIntegrationTest,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/storagetest", PackageProps)
}
