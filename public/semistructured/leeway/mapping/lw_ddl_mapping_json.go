package mapping

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	enchint "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

func LoadJsonMapping(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "blake3hash", ctabb.Y).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("bool").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.B)
	manip.TaggedValueSection("undefined").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("null").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("string").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression,
			enchint.AspectInterRecordLowCardinality,
			enchint.AspectIntraRecordLowCardinality)
	manip.TaggedValueSection("float64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.F64)
	manip.TaggedValueSection("int64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.I64)
}
func LoadJsonMappingLossless(manip common.TableManipulatorFluidI) {
	LoadJsonMapping(manip)
	manip.TaggedValueSection("emptyObject").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("emptyArray").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
}

func NewJsonMapping() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	LoadJsonMapping(manip)
	manip.SetTableName("JsonMapping")
	manip.SetTableComment("canonical et7 json mapping")
	return manip.BuildTableDesc()
}
