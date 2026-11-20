package skeleton

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
var PackageProps = packageprops.Props{
	Kind: packageprops.KindTool,
}

func init() {
	packageprops.Register("github.com/stergiotis/boxer/public/gov/skeleton", PackageProps)
}
