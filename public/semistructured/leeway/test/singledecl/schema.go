// Package singledecl is the compiled fixture of the single-membership
// declaration (ADR-0213): one schema pair — sdecl declares its channels
// single-instance, sflat is the byte-for-byte twin without the declaration —
// driven through the generated DML and read-access classes, the read-back
// artefacts, the SQL authoring surface and schema discovery, so the
// declaration's write-time enforcement and read-side fast path are pinned
// against the undeclared general form.
//
// A third table, striple, pins the facts11'-shaped four-channel section
// (lv single | lr carded | mvhp single | hv carded, list values) — the exact
// combination hackathon_2026's facts11' relocation declares (its ADR-0027).
package singledecl

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// GetSchemaInManipulator builds the fixture schema: plain id, a "tags"
// section carrying a low-card-verbatim channel beside a ragged high-card-ref
// channel, and an "addr" section on the mixed low-card-verbatim channel.
// declared adds the ADR-0213 single-instance declaration on lv and lmv —
// the only difference between sdecl and sflat.
func GetSchemaInManipulator(declared bool) (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	if declared {
		manip.SetTableName("sdecl")
	} else {
		manip.SetTableName("sflat")
	}
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)

	tags := manip.TaggedValueSection("tags").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim, common.MembershipSpecHighCardRef)
	if declared {
		tags.AddSectionSingleMembership(common.MembershipSpecLowCardVerbatim)
	}
	tags.TaggedValueColumn("value", ctabb.S)

	addr := manip.TaggedValueSection("addr").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	if declared {
		addr.AddSectionSingleMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	}
	addr.TaggedValueColumn("value", ctabb.S)
	return
}

// GetTripleSchemaInManipulator builds striple: one section carrying the
// four-channel combination a facts11'-shaped canonical section declares —
// lv (single, the semantic type) beside lr (carded, the reserved curated
// twin, empty until populated), mvhp (single, the address) and hv (carded,
// 0..N additive annotations) — over a LIST value lane, so the card-less
// list read (arraySlice over the length lane at an indexOf'd position) is
// exercised beside carded channels. hv has no other in-tree producer; this
// fixture is deliberately its first.
func GetTripleSchemaInManipulator() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("create table manipulator: %w", err)
		return
	}
	manip.SetTableName("striple")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)

	facts := manip.TaggedValueSection("facts").
		SectionStreamingGroup("data").
		AddSectionMembership(
			common.MembershipSpecLowCardVerbatim,
			common.MembershipSpecLowCardRef,
			common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
			common.MembershipSpecHighCardVerbatim,
		)
	facts.AddSectionSingleMembership(
		common.MembershipSpecLowCardVerbatim,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
	)
	facts.TaggedValueColumn("value", ctabb.Sh)
	return
}
