package providers

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/packageprops"
)

// TestPackageCapsSchema pins the columns regardless of build tags: the disabled
// build must keep the schema and drop only the rows, so a query written against
// an ordinary build still parses against a stripped one (ADR-0120 SD9).
func TestPackageCapsSchema(t *testing.T) {
	rec, err := packageCapsProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	for _, col := range []string{"import_path", "surveyed", "safe", "caps_direct", "caps_reachable"} {
		require.NotEmpty(t, rec.Schema().FieldIndices(col), "missing column %q", col)
	}
	assert.Equal(t, "package_capabilities", packageCapsProvider{}.Name())
	assert.Equal(t, introspect.FreshnessStatic, packageCapsProvider{}.Freshness())
}

// TestPackageCapsTableRendersVerdicts drives the table with fixed rows rather
// than the live registry, so it asserts the rendering rather than whatever this
// test binary happens to link.
func TestPackageCapsTableRendersVerdicts(t *testing.T) {
	rows := packageprops.Table{
		{
			ImportPath: "example.com/m/execer",
			Props: packageprops.Props{
				CapsDirect:    packageprops.Caps(packageprops.CapabilityFiles, packageprops.CapabilityExec),
				CapsReachable: packageprops.Caps(packageprops.CapabilityFiles, packageprops.CapabilityExec, packageprops.CapabilityNetwork),
			},
		},
		{
			ImportPath: "example.com/m/pure",
			Props: packageprops.Props{
				CapsDirect:    packageprops.Caps(packageprops.CapabilitySafe),
				CapsReachable: packageprops.Caps(packageprops.CapabilitySafe),
			},
		},
		{
			ImportPath: "example.com/m/unsurveyed",
			Props:      packageprops.Props{},
		},
	}
	rec := packageCapsTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()
	require.EqualValues(t, 3, rec.NumRows())

	// Ascending capability (proto enum) order, not the order they were passed to
	// Caps: files=2, network=3, exec=14.
	assert.Equal(t, []string{"files", "exec"}, stringListAt(t, rec, "caps_direct", 0))
	assert.Equal(t, []string{"files", "network", "exec"}, stringListAt(t, rec, "caps_reachable", 0))
	assert.True(t, boolAt(t, rec, "surveyed", 0))
	assert.False(t, boolAt(t, rec, "safe", 0))

	// A surveyed-but-clean package is safe and explicitly so.
	assert.Equal(t, []string{"safe"}, stringListAt(t, rec, "caps_direct", 1))
	assert.True(t, boolAt(t, rec, "surveyed", 1))
	assert.True(t, boolAt(t, rec, "safe", 1))

	// An unsurveyed package asserts nothing — and must not read as safe, which
	// is the whole point of the safe marker (ADR-0120 SD4).
	assert.Empty(t, stringListAt(t, rec, "caps_direct", 2))
	assert.False(t, boolAt(t, rec, "surveyed", 2))
	assert.False(t, boolAt(t, rec, "safe", 2), "an unsurveyed package must never read as safe")
}

// stringListAt returns row i of the named list-of-string column.
func stringListAt(t *testing.T, rec arrow.RecordBatch, col string, i int) (out []string) {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	lst := rec.Column(idx[0]).(*array.List)
	vals := lst.ListValues().(*array.String)
	start, end := lst.ValueOffsets(i)
	for j := start; j < end; j++ {
		out = append(out, vals.Value(int(j)))
	}
	return
}

// boolAt returns row i of the named Bool column.
func boolAt(t *testing.T, rec arrow.RecordBatch, col string, i int) bool {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return rec.Column(idx[0]).(*array.Boolean).Value(i)
}
