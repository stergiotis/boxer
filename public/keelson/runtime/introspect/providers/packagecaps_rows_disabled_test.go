//go:build boxer_disable_packagecaps

package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestPackageCapsRowsWithheldUnderTag is the disabled half of the ADR-0120 SD9
// opt-out, and the reason both halves exist: the tag must empty the table
// without removing it. A query against a stripped binary still has to parse and
// run — it just learns nothing.
//
// Run with: go test -tags="$(cat ./tags),boxer_disable_packagecaps" ./...
func TestPackageCapsRowsWithheldUnderTag(t *testing.T) {
	rec, err := packageCapsProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err, "a disabled table must still answer, not error")
	defer rec.Release()
	assert.Zero(t, rec.NumRows(), "boxer_disable_packagecaps must withhold every row")

	// Schema survives: this is what keeps keelson('package_capabilities') a
	// valid query rather than an unknown-table error.
	for _, col := range []string{"import_path", "surveyed", "safe", "caps_direct", "caps_reachable"} {
		require.NotEmpty(t, rec.Schema().FieldIndices(col),
			"column %q must survive the opt-out tag", col)
	}
}

// TestRegisterStaticKeepsTableUnderTag pins that the table name set does not
// depend on build flags — the provider stays registered either way.
func TestRegisterStaticKeepsTableUnderTag(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, RegisterStatic(r))
	assert.Contains(t, r.Names(), "package_capabilities",
		"the opt-out tag empties the table; it must not unregister it")
}
