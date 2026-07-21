// Package godepcollect is the live godep.SourceI adapter for the Go
// dependency explorer (ADR-0064): it loads the transitive package closure
// with golang.org/x/tools/go/packages and builds a godep.Manifest.
//
// It is the one place in the explorer that depends on the go toolchain; the
// manifest package (godep) and the app's render path do not. The
// FactsSource adapter that reads the same manifest back from boxer.facts
// is the deferred second adapter (ADR-0064 SD3/SD7).
package godepcollect
