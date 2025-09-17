package common

import (
	"github.com/stergiotis/boxer/public/observability/eh"
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
		break
	case ColumnRoleHighCardRefParametrizedCardinality:
		membershipRole = ColumnRoleHighCardRefParametrized
		break
	case ColumnRoleHighCardVerbatimCardinality:
		membershipRole = ColumnRoleHighCardVerbatim
		break
	case ColumnRoleLowCardRefCardinality:
		membershipRole = ColumnRoleLowCardRef
		break
	case ColumnRoleLowCardRefParametrizedCardinality:
		membershipRole = ColumnRoleLowCardRefParametrized
		break
	case ColumnRoleLowCardVerbatimCardinality:
		membershipRole = ColumnRoleLowCardVerbatim
		break
	case ColumnRoleMixedLowCardRefCardinality:
		membershipRole = ColumnRoleMixedLowCardRef
		break
	case ColumnRoleMixedLowCardVerbatimCardinality:
		membershipRole = ColumnRoleMixedLowCardVerbatim
		break
	default:
		err = ErrUnhandledRole
	}
	return
}
func GetCardinalityRoleByMembershipRole(membershipRole ColumnRoleE) (cardinalityRole ColumnRoleE, err error) {
	switch membershipRole {
	case ColumnRoleHighCardRef:
		cardinalityRole = ColumnRoleHighCardRefCardinality
		break
	case ColumnRoleHighCardRefParametrized:
		cardinalityRole = ColumnRoleHighCardRefParametrizedCardinality
		break
	case ColumnRoleHighCardVerbatim:
		cardinalityRole = ColumnRoleHighCardVerbatimCardinality
		break
	case ColumnRoleLowCardRef:
		cardinalityRole = ColumnRoleLowCardRefCardinality
		break
	case ColumnRoleLowCardRefParametrized:
		cardinalityRole = ColumnRoleLowCardRefParametrizedCardinality
		break
	case ColumnRoleLowCardVerbatim:
		cardinalityRole = ColumnRoleLowCardVerbatimCardinality
		break
	case ColumnRoleMixedLowCardRef, ColumnRoleMixedRefHighCardParameters:
		cardinalityRole = ColumnRoleMixedLowCardRefCardinality
		break
	case ColumnRoleMixedLowCardVerbatim, ColumnRoleMixedVerbatimHighCardParameters:
		cardinalityRole = ColumnRoleMixedLowCardVerbatimCardinality
		break
	default:
		err = ErrUnhandledRole
	}
	return
}
