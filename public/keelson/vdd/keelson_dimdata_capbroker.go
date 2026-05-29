//go:build llm_generated_opus47

package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Capbroker narrow memberships — the request/reply DTOs (GrantRequest,
// GrantReply) for runtime.cap.request. Shared columns (appId, reason)
// live in keelson_dimdata_shared.go.
//
// Cap-filter columns flatten the nested [app.SubjectFilter] struct
// (Pattern / Reason / Direction / Sticky) into peer tagged columns.
// The codec grammar is flat — nested-struct support would force a
// per-DTO sub-row encoding that doesn't fit the "one fact-kind, one
// row per call" buscodec contract. Callers reconstruct the
// SubjectFilter at the codec boundary; the broker-internal Go shape
// (capbroker.GrantRequest) is unchanged.
var (
	// MembCapFilterPattern is the NATS-style subject pattern from
	// app.SubjectFilter.Pattern (e.g. "task.*.cancel"). String section
	// because patterns are open-cardinality.
	MembCapFilterPattern = KeelsonHrNkRegistry.MustBegin("capFilterPattern").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembCapDirection is app.CapDirectionE rendered as its canonical
	// String() ("pub" / "sub" / "pub+sub" / "unspecified"). Symbol
	// section so the wire stays self-describing and the on-disk
	// column is a LowCardinality dictionary.
	MembCapDirection = KeelsonHrNkRegistry.MustBegin("capDirection").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembCapFilterSticky mirrors app.SubjectFilter.Sticky — whether
	// the grant survives an explicit revoke until a future
	// supersession arrives.
	MembCapFilterSticky = KeelsonHrNkRegistry.MustBegin("capFilterSticky").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembGrantApproved is the policy decision in the reply. False
	// means denied; the Reason column carries the rationale either
	// way. Narrow (not shared with a generic `approved`) because the
	// semantic is broker-decision-specific; a future RPC reply that
	// happens to carry an approved/denied bool can introduce its own
	// term then.
	MembGrantApproved = KeelsonHrNkRegistry.MustBegin("grantApproved").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembGrantId is the broker-local identifier the reply hands back
	// to the caller on approval (currently a uint64-as-string;
	// upstream readers don't parse it). Empty string on denial.
	MembGrantId = KeelsonHrNkRegistry.MustBegin("grantId").
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
