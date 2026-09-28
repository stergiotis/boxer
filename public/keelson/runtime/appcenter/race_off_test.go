//go:build !race

package appcenter

// raceBuild reports whether the test binary carries the race detector, whose
// instrumentation slows the ADR corpus read several-fold.
const raceBuild = false
