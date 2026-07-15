//go:build !boxer_disable_packagecaps

package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestPackageCapsRowsPresentByDefault is the enabled half of the ADR-0120 SD9
// opt-out. Every package that declares PackageProps self-registers from init, so
// this test binary links a good few of them and the table must not be empty.
func TestPackageCapsRowsPresentByDefault(t *testing.T) {
	rec, err := packageCapsProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Positive(t, rec.NumRows(),
		"the default build must report the linked packages; the rows are withheld only under boxer_disable_packagecaps")
}
