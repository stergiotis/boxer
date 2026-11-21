// Package envtest sets registry variables from tests. It is the only part
// of the registry that links the testing package: a package that reads
// variables imports public/config/env alone (ADR-0009, ADR-0394).
package envtest

import (
	"testing"

	"github.com/stergiotis/boxer/public/config/env"
)

// VarI is what Set needs of a variable; every typed variable env declares
// satisfies it.
type VarI interface {
	env.VarI
	ClearCache()
	ClearOverride()
}

// Set sets v's environment variable to value for the test (t.Setenv) and
// forgets v's cached value, recorded flag value and Override, so the next
// Get reads value. The cache is forgotten again on t.Cleanup so a later
// test starts fresh. A value a typed variable cannot parse, or a
// categorial value outside its set, is accepted here; Get then falls back
// to the default as it does for any malformed environment value.
func Set(t testing.TB, v VarI, value string) {
	t.Helper()
	reset(v)
	t.Setenv(v.Spec().Name, value)
	t.Cleanup(func() {
		reset(v)
	})
}

func reset(v VarI) {
	v.ClearCache()
	v.ClearOverride()
}
