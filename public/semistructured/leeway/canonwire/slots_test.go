package canonwire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// jsonSlotTable is the leeway JSON mapping in its lossless variant: it carries
// both halves of the SD2 ambiguity story — `string` and `symbol` share the `s`
// signature, and four sections are value-less.
func jsonSlotTable(t *testing.T) (tbl common.TableDesc, slots SlotTable) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	mapping.LoadJsonMappingLossless(manip)
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	slots, err = BuildSlotTable(&tbl)
	require.NoError(t, err)
	return
}

// sectionNames lists a slot's sections in signature order.
func sectionNames(slot Slot) (names []string) {
	names = make([]string, 0, len(slot.Sections))
	for _, s := range slot.Sections {
		names = append(names, string(s.Name))
	}
	return
}

func TestBuildSlotTableJsonMapping(t *testing.T) {
	_, slots := jsonSlotTable(t)

	// One slot per section: the JSON mapping declares no co-section group.
	require.Len(t, slots.Slots, 9)

	// Every slot's signature is its single section's group.
	got := make(map[string][]string, len(slots.BySignature))
	for sig, ordinals := range slots.BySignature {
		for _, o := range ordinals {
			require.Len(t, slots.Slots[o].Sections, 1)
			got[sig] = append(got[sig], sectionNames(slots.Slots[o])...)
		}
	}
	require.Equal(t, []string{"undefined", "null", "emptyObject", "emptyArray"}, got[""])
	require.Equal(t, []string{"string", "symbol"}, got["s"])
	require.Equal(t, []string{"bool"}, got["b"])
	require.Equal(t, []string{"float64"}, got["f64"])
	require.Equal(t, []string{"int64"}, got["i64"])

	// The ambiguity sets, and only those.
	require.Equal(t, []string{"", "s"}, slots.Ambiguous())
	require.Len(t, slots.BySignature[""], 4)
	require.Len(t, slots.BySignature["s"], 2)
	require.Len(t, slots.BySignature["b"], 1)

	// Slots are ordered by signature, then by first section index, so the
	// ordinals a generator emits do not depend on map iteration.
	sigs := make([]string, 0, len(slots.Slots))
	for _, s := range slots.Slots {
		sigs = append(sigs, s.Signature)
	}
	require.Equal(t, []string{"", "", "", "", "b", "f64", "i64", "s", "s"}, sigs)
	require.Equal(t, []int{0, 1, 2, 3}, slots.BySignature[""])
	require.Equal(t, []int{7, 8}, slots.BySignature["s"])

	// The entity-id plain: one `y` column, keyed by its item type.
	require.Len(t, slots.Plains, 1)
	require.Equal(t, common.PlainItemTypeEntityId, slots.Plains[0].ItemType)
	require.Equal(t, "y", slots.Plains[0].Group)
	require.Equal(t, []int{0}, slots.Plains[0].ColumnOrder)
	require.Equal(t, []naming.StylableName{"blake3hash"}, slots.Plains[0].Names)
}

// Every section of the JSON mapping is on the mixed-verbatim channel, so the
// narrowing step of SD5 cannot separate `string` from `symbol` — which is the
// case the tagger/dispatcher pair exists for.
func TestJsonMappingAmbiguityIsNotNarrowable(t *testing.T) {
	_, slots := jsonSlotTable(t)
	for _, o := range slots.BySignature["s"] {
		sec := slots.Slots[o].Sections[0]
		require.True(t, SpecAcceptsChannel(sec.MembershipSpec, mappingplan.MembershipChannelMixedLowCardVerbatim))
		require.Equal(t,
			[]mappingplan.MembershipChannel{mappingplan.MembershipChannelMixedLowCardVerbatim},
			SpecChannels(sec.MembershipSpec))
	}
}

// A co-section group is one slot whose signature joins its member sections'
// groups, sorted; the sections come back in that order.
func TestBuildSlotTableCoSectionGroup(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	geo := manip.TaggedValueSection("geo").
		AddSectionMembership(common.MembershipSpecLowCardRef).
		SectionCoSectionGroup("place")
	geo.TaggedValueColumn("lat", ctabb.F32)
	geo.TaggedValueColumn("lng", ctabb.F32)
	cell := manip.TaggedValueSection("h3").
		AddSectionMembership(common.MembershipSpecHighCardRef).
		SectionCoSectionGroup("place")
	cell.TaggedValueColumn("cell", ctabb.U64)
	manip.TaggedValueSection("note").
		AddSectionMembership(common.MembershipSpecLowCardRef).
		TaggedValueColumn("text", ctabb.S)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)

	slots, err := BuildSlotTable(&tbl)
	require.NoError(t, err)
	require.Len(t, slots.Slots, 2)
	require.Empty(t, slots.Ambiguous())

	co := slots.Slots[0]
	require.Equal(t, "f32-f32_u64", co.Signature)
	require.Equal(t, []string{"geo", "h3"}, sectionNames(co))
	require.Equal(t, "f32-f32", co.Sections[0].Group)
	require.Equal(t, []int{0, 1}, co.Sections[0].ColumnOrder)
	require.Equal(t, common.MembershipSpecLowCardRef, co.Sections[0].MembershipSpec)
	require.Equal(t, "u64", co.Sections[1].Group)
	require.Equal(t, common.MembershipSpecHighCardRef, co.Sections[1].MembershipSpec)

	require.Equal(t, "s", slots.Slots[1].Signature)
	require.Equal(t, []string{"note"}, sectionNames(slots.Slots[1]))
}

func TestBuildSlotTableRejectsNoTable(t *testing.T) {
	_, err := BuildSlotTable(nil)
	require.Error(t, err)
}

// The eight-way channel → spec mapping, pinned, and cross-checked against the
// carriage axes the channel itself reports. The four channels the readback
// resolver covers are the first four rows; the other four are what this
// function adds.
func TestChannelSpecCoversEveryChannel(t *testing.T) {
	cases := []struct {
		ch   mappingplan.MembershipChannel
		spec common.MembershipSpecE
	}{
		{mappingplan.MembershipChannelLowCardRef, common.MembershipSpecLowCardRef},
		{mappingplan.MembershipChannelLowCardVerbatim, common.MembershipSpecLowCardVerbatim},
		{mappingplan.MembershipChannelHighCardRef, common.MembershipSpecHighCardRef},
		{mappingplan.MembershipChannelHighCardVerbatim, common.MembershipSpecHighCardVerbatim},
		{mappingplan.MembershipChannelMixedLowCardRef, common.MembershipSpecMixedLowCardRefHighCardParameters},
		{mappingplan.MembershipChannelMixedLowCardVerbatim, common.MembershipSpecMixedLowCardVerbatimHighCardParameters},
		{mappingplan.MembershipChannelLowCardRefParametrized, common.MembershipSpecLowCardRefParametrized},
		{mappingplan.MembershipChannelHighCardRefParametrized, common.MembershipSpecHighCardRefParametrized},
	}
	require.Len(t, cases, len(AllMembershipChannels))

	var union common.MembershipSpecE
	for _, tc := range cases {
		spec, err := ChannelSpec(tc.ch)
		require.NoError(t, err)
		require.Equal(t, tc.spec, spec, tc.ch.String())

		// The mapping is injective: each channel claims a bit no other one has.
		require.Zero(t, union&spec, tc.ch.String())
		union |= spec

		// The spec's meaning agrees with the channel's carriage axes: a
		// params-bearing channel lands on a params-bearing spec, and the
		// identity encoding decides which of the two it is.
		require.Equal(t, tc.ch.HasParams(), spec.ContainsMixed() ||
			spec == common.MembershipSpecLowCardRefParametrized ||
			spec == common.MembershipSpecHighCardRefParametrized, tc.ch.String())
		switch tc.ch.Identity() {
		case membership.IdentityRef:
			require.True(t, spec.HasLowCardRefOnly() || spec.HasHighCardRefOnly(), tc.ch.String())
		case membership.IdentityVerbatim:
			require.True(t, spec.HasLowCardVerbatim() || spec.HasHighCardVerbatim(), tc.ch.String())
		case membership.IdentityPerRowId:
			require.True(t, spec.HasMixedLowCardRefHighCardParameters(), tc.ch.String())
		case membership.IdentityPerRowName:
			require.True(t, spec.HasMixedLowCardVerbatimHighCardParameters(), tc.ch.String())
		case membership.IdentityPerRowBlob:
			require.True(t, spec.HasLowCardRefParametrized() || spec.HasHighCardRefParametrized(), tc.ch.String())
		default:
			t.Fatalf("channel %v has no identity encoding", tc.ch)
		}

		// Every channel is a member of the list SpecChannels walks.
		require.Contains(t, AllMembershipChannels, tc.ch)
	}

	// The eight bits are the whole MembershipSpecE bitfield.
	require.Equal(t, common.MembershipSpecE(0xff), union)

	_, err := ChannelSpec(mappingplan.MembershipChannel(len(AllMembershipChannels)))
	require.Error(t, err)
}

func TestSpecAcceptsChannelAndInverse(t *testing.T) {
	require.Empty(t, SpecChannels(common.MembershipSpecNone))
	require.NotNil(t, SpecChannels(common.MembershipSpecNone))
	require.Equal(t, AllMembershipChannels, SpecChannels(common.MembershipSpecE(0xff)))

	spec := common.MembershipSpecLowCardRef | common.MembershipSpecHighCardVerbatim
	require.Equal(t, []mappingplan.MembershipChannel{
		mappingplan.MembershipChannelLowCardRef,
		mappingplan.MembershipChannelHighCardVerbatim,
	}, SpecChannels(spec))
	require.True(t, SpecAcceptsChannel(spec, mappingplan.MembershipChannelLowCardRef))
	require.False(t, SpecAcceptsChannel(spec, mappingplan.MembershipChannelHighCardRef))
	require.False(t, SpecAcceptsChannel(spec, mappingplan.MembershipChannel(len(AllMembershipChannels))))
}
