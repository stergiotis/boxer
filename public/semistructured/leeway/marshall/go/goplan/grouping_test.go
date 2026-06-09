//go:build llm_generated_opus47

package goplan_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// sliceCanon / roaringCanon rebuild the canonical types these grouping tests
// previously expressed via the removed GoType/IsSlice/IsRoaring fields
// (scalarCanon is shared from build_test.go, same test package).
func sliceCanon(elem string) canonicaltypes.PrimitiveAstNodeI {
	return canonicaltypes.PromoteScalarPrim(scalarCanon(elem), canonicaltypes.ScalarModifierHomogenousArray)
}
func roaringCanon() canonicaltypes.PrimitiveAstNodeI {
	return canonicaltypes.PromoteScalarPrim(goplan.RoaringElemCanonical(), canonicaltypes.ScalarModifierSet)
}

// TestComputeGroups_ScalarFirstPartition locks ADR-0008 D2's
// behaviour: within one section, scalar-shaped fields land ahead of
// non-scalar fields irrespective of DTO declaration order, and the
// order within each class is stable. Memberships are rebuilt to
// reflect the post-partition first-seen order.
func TestComputeGroups_ScalarFirstPartition(t *testing.T) {
	plan := &mappingplan.Plan{
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Bits", Canonical: roaringCanon(), LWMembership: "bits", LWSection: "u32Array"},
			{GoFieldName: "Battery", Canonical: scalarCanon("uint32"), LWMembership: "battery", LWSection: "u32Array", Flags: mappingplan.FieldFlags{Unit: true}},
			{GoFieldName: "Tags", Canonical: sliceCanon("string"), LWMembership: "tags", LWSection: "u32Array"},
			{GoFieldName: "Volt", Canonical: scalarCanon("uint32"), LWMembership: "volt", LWSection: "u32Array"},
		},
	}
	groups := goplan.ComputeGroups(plan)
	require.Len(t, groups, 1)
	g := groups[0]
	require.Len(t, g.SubColumns, 1)
	got := make([]string, 0, len(g.SubColumns[0].Fields))
	for _, f := range g.SubColumns[0].Fields {
		got = append(got, f.GoFieldName)
	}
	require.Equal(t, []string{"Battery", "Volt", "Bits", "Tags"}, got,
		"scalar fields (Battery, Volt) must precede non-scalar (Bits, Tags) with stable within-class order")

	gotMemb := make([]string, 0, len(g.Memberships))
	for _, m := range g.Memberships {
		gotMemb = append(gotMemb, m.LWMembership)
	}
	require.Equal(t, []string{"battery", "volt", "bits", "tags"}, gotMemb,
		"memberships rebuilt from post-partition first-seen order")
}

// TestComputeGroups_PreservesSectionOrder confirms that the partition
// is within-section only — section order in the output continues to
// reflect DTO declaration order of the first field in each section.
func TestComputeGroups_PreservesSectionOrder(t *testing.T) {
	plan := &mappingplan.Plan{
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Bits", Canonical: roaringCanon(), LWMembership: "bits", LWSection: "u32Array"},
			{GoFieldName: "Color", Canonical: scalarCanon("string"), LWMembership: "color", LWSection: "symbol"},
			{GoFieldName: "Battery", Canonical: scalarCanon("uint32"), LWMembership: "battery", LWSection: "u32Array", Flags: mappingplan.FieldFlags{Unit: true}},
		},
	}
	groups := goplan.ComputeGroups(plan)
	require.Len(t, groups, 2)
	require.Equal(t, "u32Array", groups[0].Section, "first-declared section keeps first slot")
	require.Equal(t, "symbol", groups[1].Section)
}
