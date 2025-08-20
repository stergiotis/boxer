package mapping

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

func LoadJsonMapping(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "blake3hash", canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringBytes}).
		AddColumnEncodingHints(encodingaspects2.AspectLightGeneralCompression)
	manip.TaggedValueSection("bool").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value",
			canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringBool})
	manip.TaggedValueSection("undefined").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("null").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("string").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8}).
		AddColumnEncodingHints(encodingaspects2.AspectLightGeneralCompression)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8}).
		AddColumnEncodingHints(encodingaspects2.AspectLightGeneralCompression,
			encodingaspects2.AspectInterRecordLowCardinality,
			encodingaspects2.AspectIntraRecordLowCardinality)
	manip.TaggedValueSection("float64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericFloat, Width: 64})
	manip.TaggedValueSection("int64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericSigned, Width: 64})
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
