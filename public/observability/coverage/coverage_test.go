package coverage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The probe must error in every test-binary flavor: uninstrumented and
// set/count-mode runs fail WriteCounters' covermode gate, and atomic-mode
// runs (e.g. -race -cover in CI) fail on meta-data finalization, which test
// binaries defer to an exit hook. If a toolchain bump ever makes this pass,
// the probe's contract changed — re-check ProbeRuntimeSupport's doc and the
// cover lane script before loosening the assertion.
func TestProbeRuntimeSupportErrsInTestBinaries(t *testing.T) {
	require.Error(t, ProbeRuntimeSupport())
}
