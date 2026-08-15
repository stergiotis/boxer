package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// Error fact-kind memberships — used by `keelson/runtime/codec/error`
// (ADR-0042 M5, the rowmarshall.Error retrofit). All are Arbitrary
// cardinality: every per-fact attribute appears N times (N = total
// facts across all error-tree streams), and the wire packs them
// grouped by kind within their section per ADR-0041's parallel-array
// layout.
var (
	MembErrorMsg = KeelsonHrNkRegistry.MustBegin("errorMsg", 13).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorSource = KeelsonHrNkRegistry.MustBegin("errorSource", 14).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorFunc = KeelsonHrNkRegistry.MustBegin("errorFunc", 15).
			MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorStreamName = KeelsonHrNkRegistry.MustBegin("errorStreamName", 16).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorLine = KeelsonHrNkRegistry.MustBegin("errorLine", 17).
			MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorFactId = KeelsonHrNkRegistry.MustBegin("errorFactId", 18).
			MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorParentId = KeelsonHrNkRegistry.MustBegin("errorParentId", 19).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembErrorData = KeelsonHrNkRegistry.MustBegin("errorData", 20).
			MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
)
