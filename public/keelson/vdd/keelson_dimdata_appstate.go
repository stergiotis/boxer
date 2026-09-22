package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// App-state manager memberships (ADR-0185 §SD3): the request an app holding
// `runtime.appstate.>` sends on `runtime.appstate.<op>` and the reply the
// host's service answers with. The state itself is not here — it lives on
// the state store under the runtime vocabulary (ADR-0105 D3a); these are the
// wire forms that cross the bus.
//
// Kind-narrow (`asReq…` / `asOutcome…`) for the reason the watchbill cohort
// gives: a request field is ExactlyOne, a reply outcome column is Arbitrary
// (one entry per kind a forget touched), and one membership cannot carry
// both. Ordinals continue after the watchbill launch cohort.
var (
	// The request. Op is the verb; AppId names the owning app for both
	// verbs. A delete also names one entry: its Kind and its Key as
	// keelson('app_state') shows them, or — for a row of a kind the host
	// does not know — the store's own EntityId.
	MembAsReqOp = KeelsonHrNkRegistry.MustBegin("asReqOp", 209).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembAsReqKind = KeelsonHrNkRegistry.MustBegin("asReqKind", 210).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembAsReqAppId = KeelsonHrNkRegistry.MustBegin("asReqAppId", 211).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembAsReqKey = KeelsonHrNkRegistry.MustBegin("asReqKey", 212).
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembAsReqEntityId = KeelsonHrNkRegistry.MustBegin("asReqEntityId", 213).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// The reply. Ok says every attempted delete landed; the shared `reason`
	// carries why not. The outcome columns are zipped by index, one entry
	// per kind the verb touched: how many entries were cleared and how many
	// failed (ADR-0185 §SD4 — a forget reports per kind, never one verdict).
	MembAsReplyOk = KeelsonHrNkRegistry.MustBegin("asReplyOk", 214).
			MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembAsOutcomeKind = KeelsonHrNkRegistry.MustBegin("asOutcomeKind", 215).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembAsOutcomeCleared = KeelsonHrNkRegistry.MustBegin("asOutcomeCleared", 216).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembAsOutcomeFailed = KeelsonHrNkRegistry.MustBegin("asOutcomeFailed", 217).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
)
