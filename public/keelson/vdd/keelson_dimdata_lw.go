package vdd

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

var (
	MembLeeway    = KeelsonHrNkRegistry.MustBegin("leeway", 43).SetVirtual().End()
	MembTableName = KeelsonHrNkRegistry.MustBegin("tableName", 44).MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembSectionName = KeelsonHrNkRegistry.MustBegin("sectionName", 45).MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembColumnName = KeelsonHrNkRegistry.MustBegin("columnName", 46).MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembStreamingGroup = KeelsonHrNkRegistry.MustBegin("streamingGroup", 47).MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCoSectionGroup = KeelsonHrNkRegistry.MustBegin("coSectionGroup", 48).MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCanonicalType = KeelsonHrNkRegistry.MustBegin("canonicalType", 49).MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembUseAspect = KeelsonHrNkRegistry.MustBegin("useAspect", 50).MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembEncodingHint = KeelsonHrNkRegistry.MustBegin("encodingHint", 51).MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembValueSemantic = KeelsonHrNkRegistry.MustBegin("valueSemantic", 52).MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()

	MembColumnScope            = KeelsonHrNkRegistry.MustBegin("columnScope", 53).MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembColumnScopeEntity      = KeelsonHrNkRegistry.MustBegin("columnScopeEntity", 54).MustAddParentsVirtual(MembColumnScope).End()
	MembColumnScopeTransaction = KeelsonHrNkRegistry.MustBegin("columnScopeTransaction", 55).MustAddParentsVirtual(MembColumnScope).End()
	MembColumnScopeOpaque      = KeelsonHrNkRegistry.MustBegin("columnScopeOpaque", 56).MustAddParentsVirtual(MembColumnScope).End()
	MembColumnScopeTagged      = KeelsonHrNkRegistry.MustBegin("columnScopeTagged", 57).MustAddParentsVirtual(MembColumnScope).End()

	MembPlainItemType                = KeelsonHrNkRegistry.MustBegin("plainItemType", 58).MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembPlainItemTypeNone            = KeelsonHrNkRegistry.MustBegin("plainItemTypeNone", 59).MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityId        = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityId", 60).MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityTimestamp = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityTimestamp", 61).MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityRouting   = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityRouting", 62).MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityLifecycle = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityLifecycle", 63).MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeTransaction     = KeelsonHrNkRegistry.MustBegin("plainItemTypeTransaction", 64).MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeOpaque          = KeelsonHrNkRegistry.MustBegin("plainItemTypeOpaque", 65).MustAddParentsVirtual(MembPlainItemType).End()

	MembColumnSubType                       = KeelsonHrNkRegistry.MustBegin("columnSubType", 66).MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembColumnSubTypeHomogenousArray        = KeelsonHrNkRegistry.MustBegin("columnSubTypeHomogenousArray", 67).MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeHomogenousArraySupport = KeelsonHrNkRegistry.MustBegin("columnSubTypeHomogenousArraySupport", 68).MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeSet                    = KeelsonHrNkRegistry.MustBegin("columnSubTypeSet", 69).MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeSetSupport             = KeelsonHrNkRegistry.MustBegin("columnSubTypeSetSupport", 70).MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeMembership             = KeelsonHrNkRegistry.MustBegin("columnSubTypeMembership", 71).MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeMembershipSupport      = KeelsonHrNkRegistry.MustBegin("columnSubTypeMembershipSupport", 72).MustAddParentsVirtual(MembColumnSubType).End()

	MembColumnRole                                   = KeelsonHrNkRegistry.MustBegin("columnRole", 73).MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembColumnRoleUnspecific                         = KeelsonHrNkRegistry.MustBegin("columnRoleUnspecific", 74).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRef                        = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRef", 75).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRefParametrized            = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRefParametrized", 76).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardVerbatim                   = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardVerbatim", 77).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRef                         = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRef", 78).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRefParametrized             = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRefParametrized", 79).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardVerbatim                    = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardVerbatim", 80).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardRef                    = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardRef", 81).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedVerbatimHighCardParameters    = KeelsonHrNkRegistry.MustBegin("columnRoleMixedVerbatimHighCardParameters", 82).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedRefHighCardParameters         = KeelsonHrNkRegistry.MustBegin("columnRoleMixedRefHighCardParameters", 83).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardVerbatim               = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardVerbatim", 84).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleValue                              = KeelsonHrNkRegistry.MustBegin("columnRoleValue", 85).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLength                             = KeelsonHrNkRegistry.MustBegin("columnRoleLength", 86).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRefCardinality             = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRefCardinality", 87).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRefParametrizedCardinality = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRefParametrizedCardinality", 88).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardVerbatimCardinality        = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardVerbatimCardinality", 89).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRefCardinality              = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRefCardinality", 90).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRefParametrizedCardinality  = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRefParametrizedCardinality", 91).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardVerbatimCardinality         = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardVerbatimCardinality", 92).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardRefCardinality         = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardRefCardinality", 93).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardVerbatimCardinality    = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardVerbatimCardinality", 94).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleCardinality                        = KeelsonHrNkRegistry.MustBegin("columnRoleCardinality", 95).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleCusumLength                        = KeelsonHrNkRegistry.MustBegin("columnRoleCusumLength", 96).MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleCusumCardinality                   = KeelsonHrNkRegistry.MustBegin("columnRoleCusumCardinality", 97).MustAddParentsVirtual(MembColumnRole).End()
)

func ResolveColumnRole(role common.ColumnRoleE) (r registry.RegisteredNaturalKey, err error) {
	switch role {
	case common.ColumnRoleUnspecific:
		r = MembColumnRoleUnspecific
	case common.ColumnRoleHighCardRef:
		r = MembColumnRoleHighCardRef
	case common.ColumnRoleHighCardRefParametrized:
		r = MembColumnRoleHighCardRefParametrized
	case common.ColumnRoleHighCardVerbatim:
		r = MembColumnRoleHighCardVerbatim
	case common.ColumnRoleLowCardRef:
		r = MembColumnRoleLowCardRef
	case common.ColumnRoleLowCardRefParametrized:
		r = MembColumnRoleLowCardRefParametrized
	case common.ColumnRoleLowCardVerbatim:
		r = MembColumnRoleLowCardVerbatim
	case common.ColumnRoleMixedLowCardRef:
		r = MembColumnRoleMixedLowCardRef
	case common.ColumnRoleMixedVerbatimHighCardParameters:
		r = MembColumnRoleMixedVerbatimHighCardParameters
	case common.ColumnRoleMixedRefHighCardParameters:
		r = MembColumnRoleMixedRefHighCardParameters
	case common.ColumnRoleMixedLowCardVerbatim:
		r = MembColumnRoleMixedLowCardVerbatim
	case common.ColumnRoleValue:
		r = MembColumnRoleValue
	case common.ColumnRoleLength:
		r = MembColumnRoleLength
	case common.ColumnRoleHighCardRefCardinality:
		r = MembColumnRoleHighCardRefCardinality
	case common.ColumnRoleHighCardRefParametrizedCardinality:
		r = MembColumnRoleHighCardRefParametrizedCardinality
	case common.ColumnRoleHighCardVerbatimCardinality:
		r = MembColumnRoleHighCardVerbatimCardinality
	case common.ColumnRoleLowCardRefCardinality:
		r = MembColumnRoleLowCardRefCardinality
	case common.ColumnRoleLowCardRefParametrizedCardinality:
		r = MembColumnRoleLowCardRefParametrizedCardinality
	case common.ColumnRoleLowCardVerbatimCardinality:
		r = MembColumnRoleLowCardVerbatimCardinality
	case common.ColumnRoleMixedLowCardRefCardinality:
		r = MembColumnRoleMixedLowCardRefCardinality
	case common.ColumnRoleMixedLowCardVerbatimCardinality:
		r = MembColumnRoleMixedLowCardVerbatimCardinality
	case common.ColumnRoleCardinality:
		r = MembColumnRoleCardinality
	case common.ColumnRoleCusumLength:
		r = MembColumnRoleCusumLength
	case common.ColumnRoleCusumCardinality:
		r = MembColumnRoleCusumCardinality
	default:
		err = eb.Build().Stringer("role", role).Errorf("unable to resolve role")
	}
	return
}

func ResolveSubType(st common.IntermediateColumnSubTypeE) (r registry.RegisteredNaturalKey, err error) {
	switch st {
	case common.IntermediateColumnsSubTypeHomogenousArray:
		r = MembColumnSubTypeHomogenousArray
	case common.IntermediateColumnsSubTypeHomogenousArraySupport:
		r = MembColumnSubTypeHomogenousArraySupport
	case common.IntermediateColumnsSubTypeSet:
		r = MembColumnSubTypeSet
	case common.IntermediateColumnsSubTypeSetSupport:
		r = MembColumnSubTypeSetSupport
	case common.IntermediateColumnsSubTypeMembership:
		r = MembColumnSubTypeMembership
	case common.IntermediateColumnsSubTypeMembershipSupport:
		r = MembColumnSubTypeMembershipSupport
	default:
		err = eb.Build().Stringer("subType", st).Errorf("unable to resolve intermediate column subtype")
	}
	return
}

func ResolvePlainItemType(pt common.PlainItemTypeE) (r registry.RegisteredNaturalKey, err error) {
	switch pt {
	case common.PlainItemTypeNone:
		r = MembPlainItemTypeNone
	case common.PlainItemTypeEntityId:
		r = MembPlainItemTypeEntityId
	case common.PlainItemTypeEntityTimestamp:
		r = MembPlainItemTypeEntityTimestamp
	case common.PlainItemTypeEntityRouting:
		r = MembPlainItemTypeEntityRouting
	case common.PlainItemTypeEntityLifecycle:
		r = MembPlainItemTypeEntityLifecycle
	case common.PlainItemTypeTransaction:
		r = MembPlainItemTypeTransaction
	case common.PlainItemTypeOpaque:
		r = MembPlainItemTypeOpaque
	default:
		err = eb.Build().Stringer("plainItemType", pt).Errorf("unable to resolve plain item type")
	}
	return
}

func ResolveColumnScope(scope common.IntermediateColumnScopeE) (r registry.RegisteredNaturalKey, err error) {
	switch scope {
	case common.IntermediateColumnScopeEntity:
		r = MembColumnScopeEntity
	case common.IntermediateColumnScopeTransaction:
		r = MembColumnScopeTransaction
	case common.IntermediateColumnScopeOpaque:
		r = MembColumnScopeOpaque
	case common.IntermediateColumnScopeTagged:
		r = MembColumnScopeTagged
	default:
		err = eb.Build().Stringer("scope", scope).Errorf("unable to resolve columns scope")
	}
	return
}
