package ddl

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

var homogenousArrayLenType = canonicalTypes.MachineNumericTypeAstNode{
	BaseType:          canonicalTypes.BaseTypeMachineNumericUnsigned,
	Width:             64,
	ByteOrderModifier: 0,
	ScalarModifier:    0,
}
var setCardinalityType = canonicalTypes.MachineNumericTypeAstNode{
	BaseType:          canonicalTypes.BaseTypeMachineNumericUnsigned,
	Width:             64,
	ByteOrderModifier: 0,
	ScalarModifier:    0,
}
var membershipRefType = canonicalTypes.MachineNumericTypeAstNode{
	BaseType:          canonicalTypes.BaseTypeMachineNumericUnsigned,
	Width:             64,
	ByteOrderModifier: 0,
	ScalarModifier:    0,
}
var membershipSerializedType = canonicalTypes.StringAstNode{
	BaseType:       canonicalTypes.BaseTypeStringBytes,
	WidthModifier:  0,
	Width:          0,
	ScalarModifier: 0,
}
var membershipVerbatimType = canonicalTypes.StringAstNode{
	BaseType:       canonicalTypes.BaseTypeStringBytes,
	WidthModifier:  0,
	Width:          0,
	ScalarModifier: 0,
}

type CanonicalColumnarRepresentation struct {
	aspectFilterFunc func(aspect encodingaspects2.AspectE) (keep bool)
}

func NewCanonicalColumnarRepresentation(aspectFilterFunc func(aspect encodingaspects2.AspectE) (keep bool)) *CanonicalColumnarRepresentation {
	return &CanonicalColumnarRepresentation{
		aspectFilterFunc: aspectFilterFunc,
	}
}

func FilterEncodingAspect(filterFunc func(aspect encodingaspects2.AspectE) (keep bool), a ...encodingaspects2.AspectE) []encodingaspects2.AspectE {
	if filterFunc == nil {
		return a
	}
	return slices.DeleteFunc(a, func(aspect encodingaspects2.AspectE) bool {
		return !filterFunc(aspect)
	})
}
func EncodingAspectFilterFuncFromTechnology(tech common.TechnologySpecificGeneratorI, minImplementationStatusIncl common.ImplementationStatusE) func(aspect encodingaspects2.AspectE) (keep bool) {
	return func(aspect encodingaspects2.AspectE) (keep bool) {
		status, _ := tech.GetEncodingHintImplementationStatus(aspect)
		return status >= minImplementationStatusIncl
	}
}

func (inst *CanonicalColumnarRepresentation) GetMembershipSetCanonicalType(s common.MembershipSpecE) (ct1 canonicalTypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicalTypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, err error) {
	if s.Count() != 1 {
		err = eb.Build().Int("bitsSet", s.Count()).Errorf("expecting exactly one bit set: %w", common.ErrInvalidMembershipSpec)
		return
	}
	filterFunc := inst.aspectFilterFunc
	if s.HasHighCardRefOnly() {
		ct1 = membershipRefType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectDeltaEncoding)...)
		colRole1 = common.ColumnRoleHighCardRef
		return
	}
	if s.HasHighCardRefParametrized() {
		ct1 = membershipSerializedType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleHighCardRefParametrized
		return
	}
	if s.HasHighCardVerbatim() {
		ct1 = membershipVerbatimType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleHighCardVerbatim
		return
	}
	if s.HasLowCardRefOnly() {
		ct1 = membershipRefType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression, encodingaspects2.AspectDeltaEncoding)...)
		colRole1 = common.ColumnRoleLowCardRef
		return
	}
	if s.HasLowCardRefParametrized() {
		ct1 = membershipSerializedType
		// NOTE: is high cardinality (parametrization is always high-card, even when the ref is low-card)
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleLowCardRefParametrized
		return
	}
	if s.HasLowCardVerbatim() {
		ct1 = membershipVerbatimType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleLowCardVerbatim
		return
	}
	if s.HasMixedLowCardRefHighCardParameters() {
		ct1 = membershipRefType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression, encodingaspects2.AspectDeltaEncoding)...)
		colRole1 = common.ColumnRoleMixedLowCardRef
		ct2 = membershipSerializedType
		hint2 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole2 = common.ColumnRoleMixedRefHighCardParameters
		return
	}
	if s.HasMixedLowCardVerbatimHighCardParameters() {
		ct1 = membershipVerbatimType
		hint1 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectInterRecordLowCardinality, encodingaspects2.AspectIntraRecordLowCardinality, encodingaspects2.AspectLightGeneralCompression)...)
		colRole1 = common.ColumnRoleMixedLowCardVerbatim
		ct2 = membershipSerializedType
		hint2 = encodingaspects2.EncodeAspectsMustValidate(FilterEncodingAspect(filterFunc, encodingaspects2.AspectLightGeneralCompression)...)
		colRole2 = common.ColumnRoleMixedVerbatimHighCardParameters
		return
	}
	return
}

var _ common.TechnologySpecificMembershipSetGenI = (*CanonicalColumnarRepresentation)(nil)
