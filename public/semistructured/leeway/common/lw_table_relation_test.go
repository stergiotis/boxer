package common

import (
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	easp "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

// buildTable materialises a TableDesc from a loader written in the same fluid
// style a schema author uses, so the tests exercise the shapes real callers
// produce rather than hand-assembled co-slices.
func buildTable(t *testing.T, name naming.StylableName, load func(manip TableManipulatorFluidI)) (tbl TableDesc) {
	t.Helper()
	manip, err := NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName(name)
	load(manip)
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	return
}

// loadGeoTable is the stand-in for a rich authored table: a plain id section
// and two tagged sections, carrying groups, memberships and aspects that a
// minimal required shape will not mention.
func loadGeoTable(manip TableManipulatorFluidI) {
	manip.PlainValueColumn(PlainItemTypeEntityId, "id", ctabb.U64).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)

	geoPoint := manip.TaggedValueSection("geoPoint").
		SectionCoSectionGroup("geo").
		SectionStreamingGroup("geo").
		AddSectionMembership(MembershipSpecLowCardVerbatim, MembershipSpecLowCardRef)
	geoPoint.TaggedValueColumn("lat", ctabb.F32).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)
	geoPoint.TaggedValueColumn("lng", ctabb.F32).
		AddColumnEncodingHints(easp.AspectLightGeneralCompression).
		AddColumnValueSemantics(valueaspects.AspectHumanReadable, valueaspects.AspectMachineReadable)

	metric := manip.TaggedValueSection("metric").
		SectionStreamingGroup("data").
		AddSectionMembership(MembershipSpecLowCardRef)
	metric.TaggedValueColumn("value", ctabb.F64).
		AddColumnValueSemantics(valueaspects.AspectScaleOfMeasurementMetricRatio)
}

// loadMapWidgetPattern is the shape a map consumer requires: one section, two
// columns, one type each. It pins no group, no membership and no aspect — the
// point of the containment rules is that this still matches the richer table.
func loadMapWidgetPattern(manip TableManipulatorFluidI) {
	geoPoint := manip.TaggedValueSection("geoPoint")
	geoPoint.TaggedValueColumn("lat", ctabb.F32)
	geoPoint.TaggedValueColumn("lng", ctabb.F32)
}

func TestIsSubsetMinimalPatternMatchesRicherTable(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	pattern := buildTable(t, "mapWidget", loadMapWidgetPattern)
	table := buildTable(t, "geoTable", loadGeoTable)

	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "expected containment, got: %s", report)
	require.Empty(t, report.Mismatches)

	// The reverse must not hold: the table declares plenty the pattern lacks.
	report, err = ops.IsSubset(&table, &pattern)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
}

func TestIsSubsetReflexive(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for range 50 {
		var tbl TableDesc
		tbl, err = GenerateSampleTableDesc(rnd, nil, nil)
		require.NoError(t, err)
		var report SubsetReport
		report, err = ops.IsSubset(&tbl, &tbl)
		require.NoError(t, err)
		require.True(t, report.IsSubset, "a table must contain itself, got: %s", report)
	}
}

// restyleNames rewrites every name in the table to one naming style. It is
// deliberately not TableNormalizer.ScrambleNames, which picks a style per name
// and so yields a mixed-style table the validator rejects outright; a table
// discovered from real physical columns carries one convention throughout.
func restyleNames(tbl *TableDesc, style naming.NamingStyleE) {
	for i, name := range tbl.PlainValuesNames {
		tbl.PlainValuesNames[i] = naming.ConvertNameStyle(name, style)
	}
	for i := range tbl.TaggedValuesSections {
		sec := &tbl.TaggedValuesSections[i]
		sec.Name = naming.ConvertNameStyle(sec.Name, style)
		for j, name := range sec.ValueColumnNames {
			sec.ValueColumnNames[j] = naming.ConvertNameStyle(name, style)
		}
	}
}

// TestIsSubsetIgnoresNamingStyleAndOrder is the property that makes the
// relation usable against a table discovered from physical column names: the
// discovered styling and column order need not match the authored ones.
func TestIsSubsetIgnoresNamingStyleAndOrder(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	normalizer := NewTableNormalizer(naming.DefaultNamingStyle)
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	table := buildTable(t, "geoTable", loadGeoTable)

	for _, style := range naming.AllNamingStyles {
		t.Run(style.String(), func(t *testing.T) {
			pattern := buildTable(t, "mapWidget", loadMapWidgetPattern)
			restyleNames(&pattern, style)
			normalizer.ScrambleOrder(&pattern, rnd)

			report, err := ops.IsSubset(&pattern, &table)
			require.NoError(t, err)
			require.True(t, report.IsSubset, "pattern in %s must still match, got: %s", style, report)
		})
	}
}

func TestIsSubsetMissingSection(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	pattern := buildTable(t, "pattern", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("absent").TaggedValueColumn("x", ctabb.F32)
	})
	table := buildTable(t, "geoTable", loadGeoTable)

	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchMissingSection, report.Mismatches[0].Kind)
	require.EqualValues(t, "absent", report.Mismatches[0].Section)
}

func TestIsSubsetMissingColumn(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	pattern := buildTable(t, "pattern", func(manip TableManipulatorFluidI) {
		geoPoint := manip.TaggedValueSection("geoPoint")
		geoPoint.TaggedValueColumn("lat", ctabb.F32)
		geoPoint.TaggedValueColumn("altitude", ctabb.F32)
	})
	table := buildTable(t, "geoTable", loadGeoTable)

	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchMissingColumn, report.Mismatches[0].Kind)
	// Reported names are normalized, not as authored — see SubsetReport.
	require.EqualValues(t, "geo-point", report.Mismatches[0].Section)
	require.EqualValues(t, "altitude", report.Mismatches[0].Column)
}

func TestIsSubsetMissingPlainColumn(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	pattern := buildTable(t, "pattern", func(manip TableManipulatorFluidI) {
		manip.PlainValueColumn(PlainItemTypeEntityId, "absentId", ctabb.U64)
	})
	table := buildTable(t, "geoTable", loadGeoTable)

	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchMissingColumn, report.Mismatches[0].Kind)
	require.Equal(t, PlainItemTypeEntityId, report.Mismatches[0].PlainItemType)
	require.EqualValues(t, "absent-id", report.Mismatches[0].Column)
}

// TestIsSubsetTypeIsExact pins the chosen policy: no widening. A consumer
// asking for f64 is not satisfied by an f32 column even though every value
// would fit, and vice versa.
func TestIsSubsetTypeIsExact(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	pattern := buildTable(t, "pattern", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("geoPoint").TaggedValueColumn("lat", ctabb.F64)
	})
	table := buildTable(t, "geoTable", loadGeoTable)

	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchType, report.Mismatches[0].Kind)
	require.Equal(t, ctabb.F64.String(), report.Mismatches[0].Want)
	require.Equal(t, ctabb.F32.String(), report.Mismatches[0].Got)
}

func TestIsSubsetAspectsMustBeCovered(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	table := buildTable(t, "geoTable", loadGeoTable)

	// A subset of the table's value semantics is covered.
	covered := buildTable(t, "covered", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("geoPoint").
			TaggedValueColumn("lat", ctabb.F32).
			AddColumnValueSemantics(valueaspects.AspectHumanReadable)
	})
	report, err := ops.IsSubset(&covered, &table)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "a subset of the semantics must be covered, got: %s", report)

	// An aspect the table's column does not carry is not.
	uncovered := buildTable(t, "uncovered", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("geoPoint").
			TaggedValueColumn("lat", ctabb.F32).
			AddColumnValueSemantics(valueaspects.AspectApplicationLevelEncryption)
	})
	report, err = ops.IsSubset(&uncovered, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchValueSemantics, report.Mismatches[0].Kind)
}

func TestIsSubsetMembershipMustBeCovered(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	table := buildTable(t, "geoTable", loadGeoTable)

	// geoPoint carries LowCardVerbatim|LowCardRef; asking for one of them holds.
	covered := buildTable(t, "covered", func(manip TableManipulatorFluidI) {
		sec := manip.TaggedValueSection("geoPoint").AddSectionMembership(MembershipSpecLowCardRef)
		sec.TaggedValueColumn("lat", ctabb.F32)
	})
	report, err := ops.IsSubset(&covered, &table)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "expected containment, got: %s", report)

	// Asking for a membership the section does not declare does not.
	uncovered := buildTable(t, "uncovered", func(manip TableManipulatorFluidI) {
		sec := manip.TaggedValueSection("geoPoint").AddSectionMembership(MembershipSpecHighCardRef)
		sec.TaggedValueColumn("lat", ctabb.F32)
	})
	report, err = ops.IsSubset(&uncovered, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchMembership, report.Mismatches[0].Kind)
}

// TestIsSubsetGroupsArePinnedOnlyWhenSet covers the asymmetry the containment
// rules deliberately introduce: an unset group asks for nothing, a set one must
// agree.
func TestIsSubsetGroupsArePinnedOnlyWhenSet(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	table := buildTable(t, "geoTable", loadGeoTable)

	// The minimal pattern pins no group and matches a table that groups geoPoint.
	pattern := buildTable(t, "mapWidget", loadMapWidgetPattern)
	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "an unset group must not constrain, got: %s", report)

	// Pinning the group the table actually uses still matches.
	agreeing := buildTable(t, "agreeing", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("geoPoint").
			SectionCoSectionGroup("geo").
			TaggedValueColumn("lat", ctabb.F32)
	})
	report, err = ops.IsSubset(&agreeing, &table)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "expected containment, got: %s", report)

	// Pinning a different group does not.
	disagreeing := buildTable(t, "disagreeing", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("geoPoint").
			SectionCoSectionGroup("elsewhere").
			TaggedValueColumn("lat", ctabb.F32)
	})
	report, err = ops.IsSubset(&disagreeing, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 1)
	require.Equal(t, SubsetMismatchGroup, report.Mismatches[0].Kind)
}

// TestIsSubsetIgnoresDictionaryEntry: a required shape does not name the table
// that satisfies it.
func TestIsSubsetIgnoresDictionaryEntry(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	a := buildTable(t, "someName", loadGeoTable)
	b := buildTable(t, "totallyDifferentName", loadGeoTable)

	report, err := ops.IsSubset(&a, &b)
	require.NoError(t, err)
	require.True(t, report.IsSubset, "the table name must not decide containment, got: %s", report)
}

func TestIsSubsetReportsEveryMismatch(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	pattern := buildTable(t, "pattern", func(manip TableManipulatorFluidI) {
		geoPoint := manip.TaggedValueSection("geoPoint")
		geoPoint.TaggedValueColumn("lat", ctabb.F64)      // wrong type
		geoPoint.TaggedValueColumn("altitude", ctabb.F32) // absent
		manip.TaggedValueSection("absent").TaggedValueColumn("x", ctabb.F32)
	})
	table := buildTable(t, "geoTable", loadGeoTable)

	report, err := ops.IsSubset(&pattern, &table)
	require.NoError(t, err)
	require.False(t, report.IsSubset)
	require.Len(t, report.Mismatches, 3, "report must be exhaustive, not fail-fast: %s", report)
}

func TestRelate(t *testing.T) {
	ops, err := NewTableOperations()
	require.NoError(t, err)
	table := buildTable(t, "geoTable", loadGeoTable)
	pattern := buildTable(t, "mapWidget", loadMapWidgetPattern)
	same := buildTable(t, "geoTableCopy", loadGeoTable)
	unrelated := buildTable(t, "unrelated", func(manip TableManipulatorFluidI) {
		manip.TaggedValueSection("audio").TaggedValueColumn("sampleRate", ctabb.U32)
	})
	// Shares geoPoint.lat with the table, but demands a column it lacks and
	// lacks columns the table has — neither contains the other.
	overlapping := buildTable(t, "overlapping", func(manip TableManipulatorFluidI) {
		geoPoint := manip.TaggedValueSection("geoPoint")
		geoPoint.TaggedValueColumn("lat", ctabb.F32)
		geoPoint.TaggedValueColumn("altitude", ctabb.F32)
	})

	for _, tc := range []struct {
		name string
		a    *TableDesc
		b    *TableDesc
		want TableRelationE
	}{
		{name: "equal", a: &table, b: &same, want: TableRelationEqual},
		{name: "subset", a: &pattern, b: &table, want: TableRelationSubset},
		{name: "superset", a: &table, b: &pattern, want: TableRelationSuperset},
		{name: "overlap", a: &overlapping, b: &table, want: TableRelationOverlap},
		{name: "disjoint", a: &unrelated, b: &table, want: TableRelationDisjoint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel, err := ops.Relate(tc.a, tc.b)
			require.NoError(t, err)
			require.Equal(t, tc.want, rel, "got %s, want %s", rel, tc.want)
		})
	}
}
