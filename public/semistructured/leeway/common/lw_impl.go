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
func GetCardinalityRoleByMembershipRole(membershipCardinalityRole ColumnRoleE) (cardinalitySrcRole ColumnRoleE, err error) {
	switch membershipCardinalityRole {
	case ColumnRoleHighCardRefCardinality:
		cardinalitySrcRole = ColumnRoleHighCardRef
		break
	case ColumnRoleHighCardRefParametrizedCardinality:
		cardinalitySrcRole = ColumnRoleHighCardRefParametrized
		break
	case ColumnRoleHighCardVerbatimCardinality:
		cardinalitySrcRole = ColumnRoleHighCardVerbatim
		break
	case ColumnRoleLowCardRefCardinality:
		cardinalitySrcRole = ColumnRoleLowCardRef
		break
	case ColumnRoleLowCardRefParametrizedCardinality:
		cardinalitySrcRole = ColumnRoleLowCardRefParametrized
		break
	case ColumnRoleLowCardVerbatimCardinality:
		cardinalitySrcRole = ColumnRoleLowCardVerbatim
		break
	case ColumnRoleMixedLowCardRefCardinality:
		cardinalitySrcRole = ColumnRoleMixedLowCardRef
		break
	case ColumnRoleMixedLowCardVerbatimCardinality:
		cardinalitySrcRole = ColumnRoleMixedLowCardVerbatim
		break
	default:
		err = ErrUnhandledRole
	}
	return
}
