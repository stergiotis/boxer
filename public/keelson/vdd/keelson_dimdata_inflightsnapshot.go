//go:build llm_generated_opus47

package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
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
	MembInflightTaskId = KeelsonHrNkRegistry.MustBegin("inflightTaskId").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightTaskKind = KeelsonHrNkRegistry.MustBegin("inflightTaskKind").
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightTitle = KeelsonHrNkRegistry.MustBegin("inflightTitle").
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightAppId = KeelsonHrNkRegistry.MustBegin("inflightAppId").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightState = KeelsonHrNkRegistry.MustBegin("inflightState").
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightCreatedAtMs = KeelsonHrNkRegistry.MustBegin("inflightCreatedAtMs").
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightLastEmitMs = KeelsonHrNkRegistry.MustBegin("inflightLastEmitMs").
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightCurrent = KeelsonHrNkRegistry.MustBegin("inflightCurrent").
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightTotal = KeelsonHrNkRegistry.MustBegin("inflightTotal").
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightUnit = KeelsonHrNkRegistry.MustBegin("inflightUnit").
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembInflightEtaMs = KeelsonHrNkRegistry.MustBegin("inflightEtaMs").
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
)
