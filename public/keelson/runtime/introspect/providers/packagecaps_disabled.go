//go:build boxer_disable_packagecaps

package providers

import "github.com/stergiotis/boxer/public/packageprops"

// packageCapsRows returns no rows: this binary was built with
// boxer_disable_packagecaps, which withholds its capability inventory
// (ADR-0120 SD9).
//
// The provider stays registered and keeps its schema, so
// keelson('package_capabilities') still parses and runs and simply returns
// nothing — the same "degrade to empty, never a query error" shape as sbom
// without a SbomPath, or build without runinfo.Init. A caller that needs to tell
// "withheld" from "no packages linked" reads the build table's tags.
//
// Only the table's rows are withheld. The declarations themselves are ordinary
// per-package source and remain compiled in; the tag governs what this binary
// will *report* about itself, not what it is.
func packageCapsRows() (rows packageprops.Table) { return nil }
