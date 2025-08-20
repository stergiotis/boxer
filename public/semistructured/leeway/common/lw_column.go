package common

import (
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

func CountMembershipColumns(memb MembershipSpecE) (r int) {
	r = memb.Count()
	if memb.HasMixedLowCardRefHighCardParameters() {
		r++
	}
	if memb.HasMixedLowCardVerbatimHighCardParameters() {
		r++
	}
	return
}

func (inst TaggedValuesSection) IsValid() bool {
	if len(inst.ValueColumnNames) != len(inst.ValueColumnTypes) || len(inst.ValueColumnNames) == 0 {
		return false
	}
	v := true
	for i, n := range inst.ValueColumnNames {
		v = v && inst.ValueColumnTypes[i].IsValid() && len(n) > 0
	}
	return v
}
func (inst TaggedValuesSection) CountScalarModifiers(s canonicalTypes2.ScalarModifierE) (r int) {
	for _, t := range inst.ValueColumnTypes {
		if !t.IsScalar() {
			mod := canonicalTypes2.ScalarModifierNone
			switch tt := t.(type) {
			case *canonicalTypes2.MachineNumericTypeAstNode:
				mod = tt.ScalarModifier
				break
			case *canonicalTypes2.TemporalTypeAstNode:
				mod = tt.ScalarModifier
				break
			case *canonicalTypes2.StringAstNode:
				mod = tt.ScalarModifier
				break
			default:
			}
			if mod == s {
				r++
			}
		}
	}
	return
}

func (inst PhysicalColumnDesc) GetCanonicalType() (ct canonicalTypes2.PrimitiveAstNodeI, err error) {
	return inst.GeneratingNamingConvention.ExtractCanonicalType(inst)
}
func (inst PhysicalColumnDesc) GetEncodingHints() (hints encodingaspects.AspectSet, err error) {
	return inst.GeneratingNamingConvention.ExtractEncodingHints(inst)
}

func (inst PhysicalColumnDesc) GetTableRowConfig() (tableRowConfig TableRowConfigE, err error) {
	return inst.GeneratingNamingConvention.ExtractTableRowConfig(inst)
}
func (inst PhysicalColumnDesc) GetPlainItemType() (plainItemType PlainItemTypeE, err error) {
	return inst.GeneratingNamingConvention.ExtractPlainItemType(inst)
}
func (inst PhysicalColumnDesc) IsValid() bool {
	if len(inst.NameComponents) > 0 &&
		len(inst.NameComponents) == len(inst.NameComponentsExplanation) &&
		inst.GeneratingNamingConvention != nil {
		_, err := inst.GetCanonicalType()
		return err == nil
	}
	return false
}
