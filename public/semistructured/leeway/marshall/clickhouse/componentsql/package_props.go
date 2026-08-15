package componentsql

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `boxer code analysis golang wasmsurvey props generate`; curate by
// hand. The same group's `props verify` reconciles it.
//
// The WASM fields are left unasserted: the survey has not been run against
// this package, and the zero value states that rather than guessing a verdict.
var PackageProps = packageprops.Props{}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql", PackageProps)
}
