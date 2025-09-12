package ddl

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

var homogenousArrayLenType = canonicaltypes.MachineNumericTypeAstNode{
	BaseType:          canonicaltypes.BaseTypeMachineNumericUnsigned,
	Width:             64,
	ByteOrderModifier: 0,
	ScalarModifier:    0,
}
var setCardinalityType = canonicaltypes.MachineNumericTypeAstNode{
	BaseType:          canonicaltypes.BaseTypeMachineNumericUnsigned,
	Width:             64,
	ByteOrderModifier: 0,
	ScalarModifier:    0,
}
var membershipRefType = canonicaltypes.MachineNumericTypeAstNode{
	BaseType:          canonicaltypes.BaseTypeMachineNumericUnsigned,
	Width:             64,
	ByteOrderModifier: 0,
	ScalarModifier:    0,
}
var membershipSerializedType = canonicaltypes.StringAstNode{
	BaseType:       canonicaltypes.BaseTypeStringBytes,
	WidthModifier:  0,
	Width:          0,
	ScalarModifier: 0,
}
var membershipVerbatimType = canonicaltypes.StringAstNode{
	BaseType:       canonicaltypes.BaseTypeStringBytes,
	WidthModifier:  0,
	Width:          0,
	ScalarModifier: 0,
}

type CanonicalColumnarRepresentation struct {
	aspectFilterFunc func(aspect encodingaspects2.AspectE) (keep bool, msg string)
}

func NewCanonicalColumnarRepresentation(aspectFilterFunc func(aspect encodingaspects2.AspectE) (keep bool, msg string)) *CanonicalColumnarRepresentation {
	return &CanonicalColumnarRepresentation{
		aspectFilterFunc: aspectFilterFunc,
	}
}

func FilterEncodingAspect(filterFunc func(aspect encodingaspects2.AspectE) (keep bool, msg string), a ...encodingaspects2.AspectE) []encodingaspects2.AspectE {
	if filterFunc == nil {
		return a
	}
	return slices.DeleteFunc(a, func(aspect encodingaspects2.AspectE) bool {
		keep, _ := filterFunc(aspect)
		return !keep
	})
}
func EncodingAspectFilterFuncFromTechnology(tech common.TechnologySpecificGeneratorI, minImplementationStatusIncl common.ImplementationStatusE) func(aspect encodingaspects2.AspectE) (keep bool, msg string) {
	return func(aspect encodingaspects2.AspectE) (keep bool, msg string) {
		status, _ := tech.GetEncodingHintImplementationStatus(aspect)
		return status >= minImplementationStatusIncl, ""
	}
}

func (inst *CanonicalColumnarRepresentation) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	if s.Count() != 1 {
		err = eb.Build().Int("bitsSet", s.Count()).Errorf("expecting exactly one bit set: %w", common.ErrInvalidMembershipSpec)
		return
	}

	filterFunc := inst.aspectFilterFunc
	if s.HasHighCardRefOnly() {
		ct1 = membershipRefType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleHighCardRef
		cardRole = common.ColumnRoleHighCardRefCardinality
		return
	}
	if s.HasHighCardRefParametrized() {
		ct1 = membershipSerializedType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleHighCardRefParametrized
		cardRole = common.ColumnRoleHighCardRefParametrizedCardinality
		return
	}
	if s.HasHighCardVerbatim() {
		ct1 = membershipVerbatimType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleHighCardVerbatim
		cardRole = common.ColumnRoleHighCardVerbatimCardinality
		return
	}
	if s.HasLowCardRefOnly() {
		ct1 = membershipRefType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression, encodingaspects2.AspectDeltaEncoding)...)
		colRole1 = common.ColumnRoleLowCardRef
		cardRole = common.ColumnRoleLowCardRefCardinality
		return
	}
	if s.HasLowCardRefParametrized() {
		ct1 = membershipSerializedType
		// NOTE: is high cardinality (parametrization is always high-card, even when the ref is low-card)
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleLowCardRefParametrized
		cardRole = common.ColumnRoleLowCardRefParametrizedCardinality
		return
	}
	if s.HasLowCardVerbatim() {
		ct1 = membershipVerbatimType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleLowCardVerbatim
		cardRole = common.ColumnRoleLowCardVerbatimCardinality
		return
	}
	if s.HasMixedLowCardRefHighCardParameters() {
		ct1 = membershipRefType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression, encodingaspects2.AspectDeltaEncoding)...)
		colRole1 = common.ColumnRoleMixedLowCardRef
		ct2 = membershipSerializedType
		hint2 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole2 = common.ColumnRoleMixedRefHighCardParameters
		cardRole = common.ColumnRoleMixedLowCardRefCardinality
		return
	}
	if s.HasMixedLowCardVerbatimHighCardParameters() {
		ct1 = membershipVerbatimType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleMixedLowCardVerbatim
		ct2 = membershipSerializedType
		hint2 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole2 = common.ColumnRoleMixedVerbatimHighCardParameters
		cardRole = common.ColumnRoleMixedLowCardVerbatimCardinality
		return
	}
	return
}

var _ common.TechnologySpecificMembershipSetGenI = (*CanonicalColumnarRepresentation)(nil)
