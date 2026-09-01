package common

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
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

// GetMembershipSpecByMembershipRole maps a membership IDENTITY column role
// to its channel bit. ok is false for every other role — the mixed
// parameter lanes included, which belong to their identity lane's channel.
func GetMembershipSpecByMembershipRole(role ColumnRoleE) (m MembershipSpecE, ok bool) {
	ok = true
	switch role {
	case ColumnRoleHighCardRef:
		m = MembershipSpecHighCardRef
	case ColumnRoleHighCardVerbatim:
		m = MembershipSpecHighCardVerbatim
	case ColumnRoleHighCardRefParametrized:
		m = MembershipSpecHighCardRefParametrized
	case ColumnRoleLowCardRef:
		m = MembershipSpecLowCardRef
	case ColumnRoleLowCardVerbatim:
		m = MembershipSpecLowCardVerbatim
	case ColumnRoleLowCardRefParametrized:
		m = MembershipSpecLowCardRefParametrized
	case ColumnRoleMixedLowCardRef:
		m = MembershipSpecMixedLowCardRefHighCardParameters
	case ColumnRoleMixedLowCardVerbatim:
		m = MembershipSpecMixedLowCardVerbatimHighCardParameters
	default:
		ok = false
	}
	return
}

// GetMembershipRoleByMembershipSpec is the inverse of
// GetMembershipSpecByMembershipRole: one channel bit to its membership
// IDENTITY column role. ok is false for MembershipSpecNone and multi-bit
// specs.
func GetMembershipRoleByMembershipSpec(m MembershipSpecE) (role ColumnRoleE, ok bool) {
	ok = true
	switch m {
	case MembershipSpecHighCardRef:
		role = ColumnRoleHighCardRef
	case MembershipSpecHighCardVerbatim:
		role = ColumnRoleHighCardVerbatim
	case MembershipSpecHighCardRefParametrized:
		role = ColumnRoleHighCardRefParametrized
	case MembershipSpecLowCardRef:
		role = ColumnRoleLowCardRef
	case MembershipSpecLowCardVerbatim:
		role = ColumnRoleLowCardVerbatim
	case MembershipSpecLowCardRefParametrized:
		role = ColumnRoleLowCardRefParametrized
	case MembershipSpecMixedLowCardRefHighCardParameters:
		role = ColumnRoleMixedLowCardRef
	case MembershipSpecMixedLowCardVerbatimHighCardParameters:
		role = ColumnRoleMixedLowCardVerbatim
	default:
		ok = false
	}
	return
}

// GetSingleMembershipAspectByMembershipSpec maps one membership channel bit
// to the section use-aspect declaring it single-instance — every attribute
// carries exactly one membership on the channel (ADR-0213). ok is false for
// MembershipSpecNone and multi-bit specs, which have no such aspect.
func GetSingleMembershipAspectByMembershipSpec(m MembershipSpecE) (a useaspects.AspectE, ok bool) {
	ok = true
	switch m {
	case MembershipSpecHighCardRef:
		a = useaspects.AspectSectionSingleMembershipHighCardRef
	case MembershipSpecHighCardVerbatim:
		a = useaspects.AspectSectionSingleMembershipHighCardVerbatim
	case MembershipSpecHighCardRefParametrized:
		a = useaspects.AspectSectionSingleMembershipHighCardRefParametrized
	case MembershipSpecLowCardRef:
		a = useaspects.AspectSectionSingleMembershipLowCardRef
	case MembershipSpecLowCardVerbatim:
		a = useaspects.AspectSectionSingleMembershipLowCardVerbatim
	case MembershipSpecLowCardRefParametrized:
		a = useaspects.AspectSectionSingleMembershipLowCardRefParametrized
	case MembershipSpecMixedLowCardRefHighCardParameters:
		a = useaspects.AspectSectionSingleMembershipMixedLowCardRefHighCardParameters
	case MembershipSpecMixedLowCardVerbatimHighCardParameters:
		a = useaspects.AspectSectionSingleMembershipMixedLowCardVerbatimHighCardParameters
	default:
		ok = false
	}
	return
}

// GetMembershipSpecBySingleMembershipAspect is the inverse of
// GetSingleMembershipAspectByMembershipSpec: the channel a single-membership
// aspect declares. ok is false for every other aspect.
func GetMembershipSpecBySingleMembershipAspect(a useaspects.AspectE) (m MembershipSpecE, ok bool) {
	ok = true
	switch a {
	case useaspects.AspectSectionSingleMembershipHighCardRef:
		m = MembershipSpecHighCardRef
	case useaspects.AspectSectionSingleMembershipHighCardVerbatim:
		m = MembershipSpecHighCardVerbatim
	case useaspects.AspectSectionSingleMembershipHighCardRefParametrized:
		m = MembershipSpecHighCardRefParametrized
	case useaspects.AspectSectionSingleMembershipLowCardRef:
		m = MembershipSpecLowCardRef
	case useaspects.AspectSectionSingleMembershipLowCardVerbatim:
		m = MembershipSpecLowCardVerbatim
	case useaspects.AspectSectionSingleMembershipLowCardRefParametrized:
		m = MembershipSpecLowCardRefParametrized
	case useaspects.AspectSectionSingleMembershipMixedLowCardRefHighCardParameters:
		m = MembershipSpecMixedLowCardRefHighCardParameters
	case useaspects.AspectSectionSingleMembershipMixedLowCardVerbatimHighCardParameters:
		m = MembershipSpecMixedLowCardVerbatimHighCardParameters
	default:
		ok = false
	}
	return
}

// SingleMembershipSpecs decodes a section's use-aspect set into the
// membership channels it declares single-instance (ADR-0213) — the inverse
// of GetSingleMembershipAspectByMembershipSpec, folded over the set.
func SingleMembershipSpecs(aspects useaspects.AspectSet) (m MembershipSpecE) {
	for _, spec := range AllMembershipSpecs {
		if spec == MembershipSpecNone {
			continue
		}
		if a, ok := GetSingleMembershipAspectByMembershipSpec(spec); ok && aspects.Contains(a) {
			m |= spec
		}
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
