package skeleton

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
var PackageProps = packageprops.Props{}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/gov/skeleton", PackageProps)
}
