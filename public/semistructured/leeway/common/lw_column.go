package common

import (
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
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
func (inst TaggedValuesSection) CountScalarModifiers(s canonicaltypes2.ScalarModifierE) (r int) {
	for _, t := range inst.ValueColumnTypes {
		if !t.IsScalar() {
			mod := canonicaltypes2.ScalarModifierNone
			switch tt := t.(type) {
			case *canonicaltypes2.MachineNumericTypeAstNode:
				mod = tt.ScalarModifier
				break
			case *canonicaltypes2.TemporalTypeAstNode:
				mod = tt.ScalarModifier
				break
			case *canonicaltypes2.StringAstNode:
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

func (inst PhysicalColumnDesc) GetCanonicalType() (ct canonicaltypes2.PrimitiveAstNodeI, err error) {
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
