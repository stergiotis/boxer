//go:build !boxer_disable_packagecaps

package providers

import "github.com/stergiotis/boxer/public/packageprops"

// packageCapsRows returns every package linked into this binary that declares a
// PackageProps, sorted by import path.
//
// This is the default. Compile with boxer_disable_packagecaps to strip the rows
// from a shipped binary — the table keeps its schema and returns nothing, so
// queries still parse and run (ADR-0120 SD9).
func packageCapsRows() (rows packageprops.Table) { return packageprops.All() }
