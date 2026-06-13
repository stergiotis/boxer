package vdd

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

var (
	MembLeeway    = KeelsonHrNkRegistry.MustBegin("leeway").SetVirtual().End()
	MembTableName = KeelsonHrNkRegistry.MustBegin("tableName").MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembSectionName = KeelsonHrNkRegistry.MustBegin("sectionName").MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembColumnName = KeelsonHrNkRegistry.MustBegin("columnName").MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembStreamingGroup = KeelsonHrNkRegistry.MustBegin("streamingGroup").MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCoSectionGroup = KeelsonHrNkRegistry.MustBegin("coSectionGroup").MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCanonicalType = KeelsonHrNkRegistry.MustBegin("canonicalType").MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembUseAspect = KeelsonHrNkRegistry.MustBegin("useAspect").MustAddParentsVirtual(MembLeeway).
			MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembEncodingHint = KeelsonHrNkRegistry.MustBegin("encodingHint").MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembValueSemantic = KeelsonHrNkRegistry.MustBegin("valueSemantic").MustAddParentsVirtual(MembLeeway).
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()

	MembColumnScope            = KeelsonHrNkRegistry.MustBegin("columnScope").MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembColumnScopeEntity      = KeelsonHrNkRegistry.MustBegin("columnScopeEntity").MustAddParentsVirtual(MembColumnScope).End()
	MembColumnScopeTransaction = KeelsonHrNkRegistry.MustBegin("columnScopeTransaction").MustAddParentsVirtual(MembColumnScope).End()
	MembColumnScopeOpaque      = KeelsonHrNkRegistry.MustBegin("columnScopeOpaque").MustAddParentsVirtual(MembColumnScope).End()
	MembColumnScopeTagged      = KeelsonHrNkRegistry.MustBegin("columnScopeTagged").MustAddParentsVirtual(MembColumnScope).End()

	MembPlainItemType                = KeelsonHrNkRegistry.MustBegin("plainItemType").MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembPlainItemTypeNone            = KeelsonHrNkRegistry.MustBegin("plainItemTypeNone").MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityId        = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityId").MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityTimestamp = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityTimestamp").MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityRouting   = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityRouting").MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeEntityLifecycle = KeelsonHrNkRegistry.MustBegin("plainItemTypeEntityLifecycle").MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeTransaction     = KeelsonHrNkRegistry.MustBegin("plainItemTypeTransaction").MustAddParentsVirtual(MembPlainItemType).End()
	MembPlainItemTypeOpaque          = KeelsonHrNkRegistry.MustBegin("plainItemTypeOpaque").MustAddParentsVirtual(MembPlainItemType).End()

	MembColumnSubType                       = KeelsonHrNkRegistry.MustBegin("columnSubType").MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembColumnSubTypeHomogenousArray        = KeelsonHrNkRegistry.MustBegin("columnSubTypeHomogenousArray").MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeHomogenousArraySupport = KeelsonHrNkRegistry.MustBegin("columnSubTypeHomogenousArraySupport").MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeSet                    = KeelsonHrNkRegistry.MustBegin("columnSubTypeSet").MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeSetSupport             = KeelsonHrNkRegistry.MustBegin("columnSubTypeSetSupport").MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeMembership             = KeelsonHrNkRegistry.MustBegin("columnSubTypeMembership").MustAddParentsVirtual(MembColumnSubType).End()
	MembColumnSubTypeMembershipSupport      = KeelsonHrNkRegistry.MustBegin("columnSubTypeMembershipSupport").MustAddParentsVirtual(MembColumnSubType).End()

	MembColumnRole                                   = KeelsonHrNkRegistry.MustBegin("columnRole").MustAddParentsVirtual(MembLeeway).SetVirtual().End()
	MembColumnRoleUnspecific                         = KeelsonHrNkRegistry.MustBegin("columnRoleUnspecific").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRef                        = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRef").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRefParametrized            = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRefParametrized").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardVerbatim                   = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardVerbatim").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRef                         = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRef").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRefParametrized             = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRefParametrized").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardVerbatim                    = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardVerbatim").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardRef                    = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardRef").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedVerbatimHighCardParameters    = KeelsonHrNkRegistry.MustBegin("columnRoleMixedVerbatimHighCardParameters").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedRefHighCardParameters         = KeelsonHrNkRegistry.MustBegin("columnRoleMixedRefHighCardParameters").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardVerbatim               = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardVerbatim").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleValue                              = KeelsonHrNkRegistry.MustBegin("columnRoleValue").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLength                             = KeelsonHrNkRegistry.MustBegin("columnRoleLength").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRefCardinality             = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRefCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardRefParametrizedCardinality = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardRefParametrizedCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleHighCardVerbatimCardinality        = KeelsonHrNkRegistry.MustBegin("columnRoleHighCardVerbatimCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRefCardinality              = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRefCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardRefParametrizedCardinality  = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardRefParametrizedCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleLowCardVerbatimCardinality         = KeelsonHrNkRegistry.MustBegin("columnRoleLowCardVerbatimCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardRefCardinality         = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardRefCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleMixedLowCardVerbatimCardinality    = KeelsonHrNkRegistry.MustBegin("columnRoleMixedLowCardVerbatimCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleCardinality                        = KeelsonHrNkRegistry.MustBegin("columnRoleCardinality").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleCusumLength                        = KeelsonHrNkRegistry.MustBegin("columnRoleCusumLength").MustAddParentsVirtual(MembColumnRole).End()
	MembColumnRoleCusumCardinality                   = KeelsonHrNkRegistry.MustBegin("columnRoleCusumCardinality").MustAddParentsVirtual(MembColumnRole).End()
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
