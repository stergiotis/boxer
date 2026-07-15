package leewaywidgets

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stretchr/testify/require"
)

// buildPattern materialises the minimal table shape a widget requires. A
// pattern is an ordinary TableDesc authored through the same fluid API as a
// real schema — it just declares far less of one.
func buildPattern(t *testing.T, load func(manip common.TableManipulatorFluidI)) (tbl common.TableDesc) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	load(manip)
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	return
}

// TestFixtureSatisfiesMapWidgetPattern is the widget-dispatch case end to end: a
// map widget's required shape is contained in the fixture table, so a host can
// offer the widget for this table without either side knowing about the other.
// The pattern names only what the widget reads — it says nothing about the
// fixture's co-section group, streaming group, memberships, encoding hints or
// its metric / entity-id columns, all of which the containment rules let the
// table carry freely.
func TestFixtureSatisfiesMapWidgetPattern(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	fixture, err := BuildFixtureTableDesc()
	require.NoError(t, err)

	mapWidget := buildPattern(t, func(manip common.TableManipulatorFluidI) {
		geoPoint := manip.TaggedValueSection("geoPoint")
		geoPoint.TaggedValueColumn("lat", ctabb.F32)
		geoPoint.TaggedValueColumn("lng", ctabb.F32)
	})

	report, err := ops.IsSubset(&mapWidget, &fixture)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "map widget must match the fixture, got: %s", report)
}

// TestFixtureRejectsUnsatisfiablePatterns pins the two ways a widget is turned
// away, and that the report says which.
func TestFixtureRejectsUnsatisfiablePatterns(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	fixture, err := BuildFixtureTableDesc()
	require.NoError(t, err)

	t.Run("column the table lacks", func(t *testing.T) {
		altitude := buildPattern(t, func(manip common.TableManipulatorFluidI) {
			manip.TaggedValueSection("geoPoint").TaggedValueColumn("altitude", ctabb.F32)
		})
		report, err := ops.IsSubset(&altitude, &fixture)
		require.NoError(t, err)
		require.False(t, report.IsSubset)
		require.Len(t, report.Mismatches, 1)
		require.Equal(t, common.SubsetMismatchMissingColumn, report.Mismatches[0].Kind)
	})

	t.Run("type the table carries differently", func(t *testing.T) {
		// The fixture's lat is f32; a widget reading f64 is not satisfied by it.
		wideLat := buildPattern(t, func(manip common.TableManipulatorFluidI) {
			manip.TaggedValueSection("geoPoint").TaggedValueColumn("lat", ctabb.F64)
		})
		report, err := ops.IsSubset(&wideLat, &fixture)
		require.NoError(t, err)
		require.False(t, report.IsSubset)
		require.Len(t, report.Mismatches, 1)
		require.Equal(t, common.SubsetMismatchType, report.Mismatches[0].Kind)
	})
}

// TestFixtureRelatesToMetricPattern shows the coarse relation on a pattern that
// reads a section the fixture has and a column it does not: neither table
// contains the other, but they are not strangers either.
func TestFixtureRelatesToMetricPattern(t *testing.T) {
	ops, err := common.NewTableOperations()
	require.NoError(t, err)
	fixture, err := BuildFixtureTableDesc()
	require.NoError(t, err)

	partial := buildPattern(t, func(manip common.TableManipulatorFluidI) {
		metric := manip.TaggedValueSection("metric")
		metric.TaggedValueColumn("value", ctabb.F64)
		metric.TaggedValueColumn("stddev", ctabb.F64)
	})
	rel, err := ops.Relate(&partial, &fixture)
	require.NoError(t, err)
	require.Equal(t, common.TableRelationOverlap, rel, "got %s", rel)

	unrelated := buildPattern(t, func(manip common.TableManipulatorFluidI) {
		manip.TaggedValueSection("audio").TaggedValueColumn("sampleRate", ctabb.U32)
	})
	rel, err = ops.Relate(&unrelated, &fixture)
	require.NoError(t, err)
	require.Equal(t, common.TableRelationDisjoint, rel, "got %s", rel)
}
