package mapping

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

func NewCborMapping() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	pathMembershipSpec := common.MembershipSpecNone.
		AddMixedLowCardVerbatimHighCardParameters().
		AddHighCardRefOnly()
	var hintsString, hintsDate32, hintsFloat64, hintsFloat32, hintsFloat16, hintsInt64, hintsInt32, hintsInt16, hintsInt8, hintsUint64, hintsUint32, hintsUint16, hintsUint8, hintsId encodingaspects2.AspectSet
	hintsString, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsDate32, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsFloat64, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsFloat32, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsFloat16, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsInt64, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsInt32, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsInt16, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsInt8, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsUint64, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsUint32, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsUint16, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsUint8, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsId, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", canonicalTypes2.MachineNumericTypeAstNode{
		BaseType:          canonicalTypes2.BaseTypeMachineNumericUnsigned,
		Width:             64,
		ByteOrderModifier: 0,
		ScalarModifier:    0,
	}, hintsId, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("bool",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringBool},
		encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueSection("undefined",
		useaspects.EmptyAspectSet,
		pathMembershipSpec, "", "")
	manip.MergeTaggedValueSection("null",
		useaspects.EmptyAspectSet,
		pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("string",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8},
		hintsString, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("bytes",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringBytes},
		hintsString, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("date32", "value",
		canonicalTypes2.TemporalTypeAstNode{
			BaseType:       canonicalTypes2.BaseTypeTemporalUtcDatetime,
			Width:          32,
			ScalarModifier: canonicalTypes2.ScalarModifierNone,
		},
		hintsDate32, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	var _ = hintsFloat16
	//manip.MergeTaggedValueColumn("float16",
	//	"value",
	//	canonicalTypes.MachineNumericTypeAstNode{BaseType: canonicalTypes.BaseTypeMachineNumericFloat, Width: 16},
	//	hintsFloat16,
	//	useaspects.EmptyAspectSet,
	//	pathMembershipSpec)
	manip.MergeTaggedValueColumn("float32",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericFloat, Width: 64},
		hintsFloat32, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("float64",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericFloat, Width: 64},
		hintsFloat64, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("int64",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericSigned, Width: 64},
		hintsInt64, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("int32",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericSigned, Width: 32},
		hintsInt32, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("int16",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericSigned, Width: 16},
		hintsInt16, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("int8",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericSigned, Width: 8},
		hintsInt8, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("uint64",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericUnsigned, Width: 64},
		hintsUint64, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("uint32",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericUnsigned, Width: 32},
		hintsUint32, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("uint16",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericUnsigned, Width: 16},
		hintsUint16, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	manip.MergeTaggedValueColumn("uint8",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericUnsigned, Width: 8},
		hintsUint8, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec,
		"",
		"")
	return manip.BuildTableDesc()
}
