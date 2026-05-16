package common

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

var ErrUnhandledRole = eh.Errorf("unhandled role")

func (inst ColumnRoleE) IsCardinalityRole() bool {
	switch inst {
	case ColumnRoleHighCardRefCardinality,
		ColumnRoleHighCardRefParametrizedCardinality,
		ColumnRoleHighCardVerbatimCardinality,
		ColumnRoleLowCardRefCardinality,
		ColumnRoleLowCardRefParametrizedCardinality,
		ColumnRoleLowCardVerbatimCardinality,
		ColumnRoleMixedLowCardRefCardinality,
		ColumnRoleMixedLowCardVerbatimCardinality:
		return true
	}
	return false
}
func GetMembershipRoleByCardinalityRole(membershipCardinalityRole ColumnRoleE) (membershipRole ColumnRoleE, err error) {
	switch membershipCardinalityRole {
	case ColumnRoleHighCardRefCardinality:
		membershipRole = ColumnRoleHighCardRef
	case ColumnRoleHighCardRefParametrizedCardinality:
		membershipRole = ColumnRoleHighCardRefParametrized
	case ColumnRoleHighCardVerbatimCardinality:
		membershipRole = ColumnRoleHighCardVerbatim
	case ColumnRoleLowCardRefCardinality:
		membershipRole = ColumnRoleLowCardRef
	case ColumnRoleLowCardRefParametrizedCardinality:
		membershipRole = ColumnRoleLowCardRefParametrized
	case ColumnRoleLowCardVerbatimCardinality:
		membershipRole = ColumnRoleLowCardVerbatim
	case ColumnRoleMixedLowCardRefCardinality:
		membershipRole = ColumnRoleMixedLowCardRef
	case ColumnRoleMixedLowCardVerbatimCardinality:
		membershipRole = ColumnRoleMixedLowCardVerbatim
	default:
		err = ErrUnhandledRole
	}
	return
}
func GetCardinalityRoleByMembershipRole(membershipRole ColumnRoleE) (cardinalityRole ColumnRoleE, err error) {
	switch membershipRole {
	case ColumnRoleHighCardRef:
		cardinalityRole = ColumnRoleHighCardRefCardinality
	case ColumnRoleHighCardRefParametrized:
		cardinalityRole = ColumnRoleHighCardRefParametrizedCardinality
	case ColumnRoleHighCardVerbatim:
		cardinalityRole = ColumnRoleHighCardVerbatimCardinality
	case ColumnRoleLowCardRef:
		cardinalityRole = ColumnRoleLowCardRefCardinality
	case ColumnRoleLowCardRefParametrized:
		cardinalityRole = ColumnRoleLowCardRefParametrizedCardinality
	case ColumnRoleLowCardVerbatim:
		cardinalityRole = ColumnRoleLowCardVerbatimCardinality
	case ColumnRoleMixedLowCardRef, ColumnRoleMixedRefHighCardParameters:
		cardinalityRole = ColumnRoleMixedLowCardRefCardinality
	case ColumnRoleMixedLowCardVerbatim, ColumnRoleMixedVerbatimHighCardParameters:
		cardinalityRole = ColumnRoleMixedLowCardVerbatimCardinality
	default:
		err = ErrUnhandledRole
	}
	return
}
func GetSubTypeByScalarModifier(scalarModifier canonicaltypes.ScalarModifierE) (subType IntermediateColumnSubTypeE) {
	switch scalarModifier {
	case canonicaltypes.ScalarModifierNone:
		subType = IntermediateColumnsSubTypeScalar
	case canonicaltypes.ScalarModifierSet:
		subType = IntermediateColumnsSubTypeSet
	case canonicaltypes.ScalarModifierHomogenousArray:
		subType = IntermediateColumnsSubTypeHomogenousArray
	default:
		log.Panic().Stringer("scalarModifier", scalarModifier).Msg("encountered unimplemented scalar modifier")
	}
	return
}
