package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// Watchbill client-protocol memberships (ADR-0234): the request DTO an app
// publishes on `watchbill.job.<op>` and the reply the worker answers with.
// The job row itself is not here — it lives on the store-owned watchbill
// table under the runtime vocabulary (ADR-0223 §SD1); these are the wire
// forms that cross the bus, and they mirror River's client verbs (insert,
// cancel, retry, get, list) so the feature set stays mappable.
//
// Every membership is kind-narrow (`wbReq…` / `wbJob…`) for the reason the
// inflight surface gives: a request field is ExactlyOne, a reply column is
// Arbitrary (one entry per listed job), and one membership cannot carry
// both. Ordinals continue after the tally launch cohort; a later surface
// takes the next unused ones.
var (
	// The request. Op is the verb; Id names the job for cancel, retry and
	// get; the rest describe a job to enqueue or a filter to list by.
	MembWbReqOp = KeelsonHrNkRegistry.MustBegin("wbReqOp", 156).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqId = KeelsonHrNkRegistry.MustBegin("wbReqId", 157).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqKind = KeelsonHrNkRegistry.MustBegin("wbReqKind", 158).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqSubject = KeelsonHrNkRegistry.MustBegin("wbReqSubject", 159).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqQueue = KeelsonHrNkRegistry.MustBegin("wbReqQueue", 160).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqPriority = KeelsonHrNkRegistry.MustBegin("wbReqPriority", 161).
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqMaxAttempts = KeelsonHrNkRegistry.MustBegin("wbReqMaxAttempts", 162).
				MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqBackoff = KeelsonHrNkRegistry.MustBegin("wbReqBackoff", 163).
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqBackoffBaseMs = KeelsonHrNkRegistry.MustBegin("wbReqBackoffBaseMs", 164).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqTimeoutMs = KeelsonHrNkRegistry.MustBegin("wbReqTimeoutMs", 165).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqArgsKind = KeelsonHrNkRegistry.MustBegin("wbReqArgsKind", 166).
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqArgs = KeelsonHrNkRegistry.MustBegin("wbReqArgs", 167).
			MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqRunAfterMs = KeelsonHrNkRegistry.MustBegin("wbReqRunAfterMs", 168).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqStates = KeelsonHrNkRegistry.MustBegin("wbReqStates", 169).
			MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbReqKinds = KeelsonHrNkRegistry.MustBegin("wbReqKinds", 170).
			MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbReqLimit = KeelsonHrNkRegistry.MustBegin("wbReqLimit", 171).
			MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbReqNote = KeelsonHrNkRegistry.MustBegin("wbReqNote", 172).
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// The reply: one entry per job across parallel columns, the shape of
	// the inflight snapshot. Ok says the verb applied; the shared `reason`
	// carries why it did not.
	MembWbJobId = KeelsonHrNkRegistry.MustBegin("wbJobId", 173).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobKind = KeelsonHrNkRegistry.MustBegin("wbJobKind", 174).
			MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobSubject = KeelsonHrNkRegistry.MustBegin("wbJobSubject", 175).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobQueue = KeelsonHrNkRegistry.MustBegin("wbJobQueue", 176).
			MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobPriority = KeelsonHrNkRegistry.MustBegin("wbJobPriority", 177).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobState = KeelsonHrNkRegistry.MustBegin("wbJobState", 178).
			MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobAttempt = KeelsonHrNkRegistry.MustBegin("wbJobAttempt", 179).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobMaxAttempts = KeelsonHrNkRegistry.MustBegin("wbJobMaxAttempts", 180).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobRunAfterMs = KeelsonHrNkRegistry.MustBegin("wbJobRunAfterMs", 181).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobWorkerRun = KeelsonHrNkRegistry.MustBegin("wbJobWorkerRun", 182).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobFinishedAtMs = KeelsonHrNkRegistry.MustBegin("wbJobFinishedAtMs", 183).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobLastError = KeelsonHrNkRegistry.MustBegin("wbJobLastError", 184).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobOwnerApp = KeelsonHrNkRegistry.MustBegin("wbJobOwnerApp", 185).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobRequesterRun = KeelsonHrNkRegistry.MustBegin("wbJobRequesterRun", 186).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobArgsKind = KeelsonHrNkRegistry.MustBegin("wbJobArgsKind", 187).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbReplyOk = KeelsonHrNkRegistry.MustBegin("wbReplyOk", 188).
			MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// The policy columns, so a reply shows how a job may be retried
	// without a second read.
	MembWbJobBackoff = KeelsonHrNkRegistry.MustBegin("wbJobBackoff", 189).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobBackoffBaseMs = KeelsonHrNkRegistry.MustBegin("wbJobBackoffBaseMs", 190).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembWbJobTimeoutMs = KeelsonHrNkRegistry.MustBegin("wbJobTimeoutMs", 191).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
)

// The watchbill management app's launch config (ADR-0236 §SD4): what a
// caller asks the window to show — one job, or a filter. Scalars only, so
// the ExactlyOne cardinality of every other launch config holds.
var (
	MembWbLaunchJobId = KeelsonHrNkRegistry.MustBegin("wbLaunchJobId", 192).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbLaunchKind = KeelsonHrNkRegistry.MustBegin("wbLaunchKind", 193).
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWbLaunchState = KeelsonHrNkRegistry.MustBegin("wbLaunchState", 194).
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
