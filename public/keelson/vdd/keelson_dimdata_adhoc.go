package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// Ad-hoc dataset memberships (ADR-0240 §SD8): the request an app publishes
// on `adhoc.publish` / `adhoc.resolve` / `adhoc.retract`, the service's
// reply, the `adhoc.event.*` announcement, and the bus's own
// `runtime.instance.closed` (§SD5). One request kind carries the three
// verbs under an op symbol and one reply kind answers all three, the
// watchbill shape (ADR-0234): a verb that needs a field leaves the others
// zero.
//
// Every membership is kind-narrow (`adhoc…`) and ExactlyOne — a request or
// reply is about one dataset. Shared columns come from
// keelson_dimdata_shared.go: `reason`, `appId` for a publisher or a closed
// instance's app, and `tileKey` for the instance key itself (the host's
// window/tile identifier, which is what an instance key is). Ordinals
// continue after the play-launch datasets column; a later surface takes
// the next unused ones.
var (
	// MembAdhocReqOp is the verb: publish, resolve or retract.
	MembAdhocReqOp = KeelsonHrNkRegistry.MustBegin("adhocReqOp", 196).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocAlias is the stable alias a dataset is published under and
	// a consumer resolves. Bare identifiers, few per deployment.
	MembAdhocAlias = KeelsonHrNkRegistry.MustBegin("adhocAlias", 197).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocHandle is the unguessable dataset handle: the one a
	// republish or retract names, the one a reply or event carries.
	MembAdhocHandle = KeelsonHrNkRegistry.MustBegin("adhocHandle", 198).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocKeepAfterClose marks a publish as the app's rather than the
	// window's (ADR-0240 §SD5).
	MembAdhocKeepAfterClose = KeelsonHrNkRegistry.MustBegin("adhocKeepAfterClose", 199).
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocArrowStream is the Arrow IPC stream a publish carries.
	MembAdhocArrowStream = KeelsonHrNkRegistry.MustBegin("adhocArrowStream", 200).
				MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocReplyOk is true when the verb applied; false with a reason.
	MembAdhocReplyOk = KeelsonHrNkRegistry.MustBegin("adhocReplyOk", 201).
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocRevision is the dataset revision a publish produced, a
	// resolve found, or an event announces.
	MembAdhocRevision = KeelsonHrNkRegistry.MustBegin("adhocRevision", 202).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocRows is the dataset's row count.
	MembAdhocRows = KeelsonHrNkRegistry.MustBegin("adhocRows", 203).
			MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocBytes is the dataset's sealed size.
	MembAdhocBytes = KeelsonHrNkRegistry.MustBegin("adhocBytes", 204).
			MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocCreatedAtUs is the dataset's creation instant, unix
	// microseconds — what alias resolution orders by.
	MembAdhocCreatedAtUs = KeelsonHrNkRegistry.MustBegin("adhocCreatedAtUs", 205).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocHandleLive answers a resolve that asked about a bound
	// handle: whether it is still live (ADR-0188 §SD3).
	MembAdhocHandleLive = KeelsonHrNkRegistry.MustBegin("adhocHandleLive", 206).
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocNoLive marks a failed resolve as "nothing live under the
	// alias" rather than a malformed request, so the caller waits instead
	// of retrying.
	MembAdhocNoLive = KeelsonHrNkRegistry.MustBegin("adhocNoLive", 207).
			MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembAdhocEventOp is the announced transition: published or retracted.
	MembAdhocEventOp = KeelsonHrNkRegistry.MustBegin("adhocEventOp", 208).
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
