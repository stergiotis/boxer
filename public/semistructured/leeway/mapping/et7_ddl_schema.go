package mapping

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

func addString(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("string",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
	return
}
func addText(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectHeavyGeneralCompression)
	manip.MergeTaggedValueColumn("text",
		"english",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EncodeAspectsMustValidate(useaspects.AspectDocumentation),
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addSymbol(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("symbol",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addSymbolRef(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("symbol",
		"ref",
		canonicalTypes2.MachineNumericTypeAstNode{
			BaseType:          canonicalTypes2.BaseTypeMachineNumericUnsigned,
			Width:             64,
			ByteOrderModifier: 0,
			ScalarModifier:    0,
		},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addDate32(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("date32", "value",
		canonicalTypes2.TemporalTypeAstNode{
			BaseType:       canonicalTypes2.BaseTypeTemporalUtcDatetime,
			Width:          32,
			ScalarModifier: canonicalTypes2.ScalarModifierNone,
		},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addInt64(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectLightBiasSmallInteger, encodingaspects2.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("int64",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericSigned, Width: 64},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addUint64(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectLightBiasSmallInteger, encodingaspects2.AspectLightGeneralCompression)
	manip.MergeTaggedValueColumn("uint64",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericUnsigned, Width: 64},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addBool(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectNone)
	manip.MergeTaggedValueColumn("bool",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringBool},
		hints, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardVerbatimHighCardParameters,
		"",
		"")
}
func addKey(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "key", canonicalTypes2.MachineNumericTypeAstNode{
		BaseType:       canonicalTypes2.BaseTypeMachineNumericUnsigned,
		Width:          64,
		ScalarModifier: 0,
	}, hints, valueaspects.EmptyAspectSet)
}
func addContentAddressableId(manip *common.TableManipulator) {
	hints := encodingaspects2.EncodeAspectsMustValidate()
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "hash", canonicalTypes2.StringAstNode{
		BaseType:       canonicalTypes2.BaseTypeStringBytes,
		WidthModifier:  canonicalTypes2.WidthModifierFixed,
		Width:          256 / 8,
		ScalarModifier: 0,
	}, hints, valueaspects.EmptyAspectSet)
}

func NewInformationSchemaVcsManagedDimensionMapping() (dimension common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	addKey(manip)
	addSymbol(manip)
	addString(manip)
	addText(manip)
	addBool(manip)
	addUint64(manip)
	addInt64(manip)
	addDate32(manip)

	return manip.BuildTableDesc()
}

func NewInformationSchemaFactsMapping() (schema common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	addContentAddressableId(manip)
	addSymbolRef(manip)
	addString(manip)
	addText(manip)
	addBool(manip)
	addUint64(manip)
	addInt64(manip)
	addDate32(manip)
	return manip.BuildTableDesc()
}
