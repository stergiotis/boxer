package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// CapabilityGrant memberships — used by `keelson/runtime/codec/capabilitygrant`
// (ADR-0042 M4, the rowmarshall retrofit). These supersede the
// hardcoded `KindSubject = 101` … constants in
// `runtime/rowmarshall/kinds.go`; the rowmarshall package keeps those
// constants in place until the legacy writer is retired in a
// follow-up commit.
var (
	MembCgSubject = KeelsonHrNkRegistry.MustBegin("cgSubject").
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCgCapability = KeelsonHrNkRegistry.MustBegin("cgCapability").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCgValidity = KeelsonHrNkRegistry.MustBegin("cgValidity").
			MustAddRestriction("u32Range", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCgActive = KeelsonHrNkRegistry.MustBegin("cgActive").
			MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembCgGranter = KeelsonHrNkRegistry.MustBegin("cgGranter").
			MustAddRestriction("foreignKey", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
)
