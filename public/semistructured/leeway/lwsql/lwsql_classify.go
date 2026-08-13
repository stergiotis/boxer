package lwsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// The lane-role classification the transform contract's two validation
// halves share (ADR-0181 §SD5). LwShapeCheck and AuditQueries must agree on
// what a role means; keeping one classifier prevents the two from drifting
// when the role model grows, and makes an unlisted role a loud error in both
// instead of a silent mis-bucket.

// LaneKindE is the audit/shape classification of a column role.
type LaneKindE uint8

const (
	LaneKindUnknown LaneKindE = iota
	LaneKindValue
	LaneKindLength                // `len` — array value support
	LaneKindSetCardinality        // `card` — set value support
	LaneKindMembership            // membership identity/payload lanes
	LaneKindMembershipCardinality // `<role>card`
	LaneKindCusum                 // materialized cumulative companions
)

// membershipLaneRoles are the membership identity/payload lane roles; their
// cardinality companions are the `<role>card` spellings.
var membershipLaneRoles = map[common.ColumnRoleE]bool{
	common.ColumnRoleHighCardRef:                     true,
	common.ColumnRoleHighCardRefParametrized:         true,
	common.ColumnRoleHighCardVerbatim:                true,
	common.ColumnRoleLowCardRef:                      true,
	common.ColumnRoleLowCardRefParametrized:          true,
	common.ColumnRoleLowCardVerbatim:                 true,
	common.ColumnRoleMixedLowCardRef:                 true,
	common.ColumnRoleMixedLowCardVerbatim:            true,
	common.ColumnRoleMixedVerbatimHighCardParameters: true,
	common.ColumnRoleMixedRefHighCardParameters:      true,
}

// ClassifyLaneRole buckets a parsed column role. base is meaningful for
// LaneKindMembershipCardinality only: the identity-lane role the cardinality
// lane describes. Roles outside the model report LaneKindUnknown — callers
// must treat that loudly, never guess.
func ClassifyLaneRole(role common.ColumnRoleE) (kind LaneKindE, base common.ColumnRoleE) {
	switch {
	case role == common.ColumnRoleValue:
		return LaneKindValue, common.ColumnRoleUnspecific
	case role == common.ColumnRoleLength:
		return LaneKindLength, common.ColumnRoleUnspecific
	case role == common.ColumnRoleCardinality:
		return LaneKindSetCardinality, common.ColumnRoleUnspecific
	case role == common.ColumnRoleCusumLength, role == common.ColumnRoleCusumCardinality:
		return LaneKindCusum, common.ColumnRoleUnspecific
	case membershipLaneRoles[role]:
		return LaneKindMembership, common.ColumnRoleUnspecific
	case strings.HasSuffix(string(role), "card"):
		base = common.ColumnRoleE(strings.TrimSuffix(string(role), "card"))
		if membershipLaneRoles[base] {
			return LaneKindMembershipCardinality, base
		}
	}
	return LaneKindUnknown, common.ColumnRoleUnspecific
}

// MembershipParamPartner maps a mixed membership's parameter lane to the
// identity lane whose `<role>card` descriptor covers both lanes.
func MembershipParamPartner(role common.ColumnRoleE) (partner common.ColumnRoleE, isParam bool) {
	switch role {
	case common.ColumnRoleMixedVerbatimHighCardParameters:
		return common.ColumnRoleMixedLowCardVerbatim, true
	case common.ColumnRoleMixedRefHighCardParameters:
		return common.ColumnRoleMixedLowCardRef, true
	}
	return common.ColumnRoleUnspecific, false
}
