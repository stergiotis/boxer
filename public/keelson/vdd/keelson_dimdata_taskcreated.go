package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// TaskCreated narrow memberships — the creation-specific half of the
// keelson/runtime/codec/taskcreated DTO. Shared columns (taskId, title,
// appId, tileKey, runId) live in keelson_dimdata_shared.go.
//
// Cardinality is ExactlyOne for every field, matching the TaskProgress
// precedent: zero values carry sentinel semantics (taskKind="" → unset,
// taskEstimatedMs=0 → no estimate). Keeps the wire compact for the
// common case where every field has a real value.
var (
	MembTaskKind = KeelsonHrNkRegistry.MustBegin("taskKind").
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTaskCancellableB = KeelsonHrNkRegistry.MustBegin("taskCancellableB").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTaskEstimatedMs = KeelsonHrNkRegistry.MustBegin("taskEstimatedMs").
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
