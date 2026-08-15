package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// InflightSnapshotReply narrow memberships — the supervisor's list-
// inflight reply DTO. Each entry-field becomes a parallel `[]T`
// column under an Arbitrary-cardinality membership; the wrapper kind
// carries one row per reply with all N entries flattened across the
// parallel columns.
//
// Parallel-array list pattern: the leeway codec is "one fact-kind per
// row" and doesn't natively model a list-of-structs. The wrapper kind
// emits N attributes per parallel-array field per row; slice-order is
// preserved on both Marshal and Unmarshal, so the entries zip
// correctly by index on reconstruction. Each membership is distinct
// (`inflight…`) so the read-side classifier separates the parallel
// streams even when several share the same section (multiple string
// columns under `stringArray`, multiple symbol columns under `symbol`,
// etc.).
//
// All memberships are kind-narrow (`inflight…` prefix) rather than
// reusing the shared `MembTaskId` / `MembAppId` / etc. because the
// underlying cardinality differs: TaskProgress.TaskId is ExactlyOne;
// InflightSnapshotReply's entry id-column is Arbitrary. The same
// membership can't carry two cardinality declarations, so the
// inflight surface gets its own vocab.
var (
	MembInflightTaskId = KeelsonHrNkRegistry.MustBegin("inflightTaskId", 32).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightTaskKind = KeelsonHrNkRegistry.MustBegin("inflightTaskKind", 33).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightTitle = KeelsonHrNkRegistry.MustBegin("inflightTitle", 34).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightAppId = KeelsonHrNkRegistry.MustBegin("inflightAppId", 35).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightState = KeelsonHrNkRegistry.MustBegin("inflightState", 36).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightCreatedAtMs = KeelsonHrNkRegistry.MustBegin("inflightCreatedAtMs", 37).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightLastEmitMs = KeelsonHrNkRegistry.MustBegin("inflightLastEmitMs", 38).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightCurrent = KeelsonHrNkRegistry.MustBegin("inflightCurrent", 39).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightTotal = KeelsonHrNkRegistry.MustBegin("inflightTotal", 40).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightUnit = KeelsonHrNkRegistry.MustBegin("inflightUnit", 41).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightEtaMs = KeelsonHrNkRegistry.MustBegin("inflightEtaMs", 42).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
)
