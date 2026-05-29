//go:build llm_generated_opus47

package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// TaskProgress narrow memberships — the progress-specific half of the
// keelson/runtime/codec/taskprogress DTO. Shared columns (taskId, note)
// live in keelson_dimdata_shared.go; everything below is unique to the
// progress wire shape.
//
// Cardinality is ExactlyOne for every field — the Go zero value carries
// semantics (Total=0 ⇒ indeterminate task, EtaMs=0 ⇒ not-yet-computed,
// Unit="unspecified" ⇒ caller did not declare a unit). This mirrors
// capabilitygrant's "0 = no TTL" treatment of ExpiresAt instead of the
// option.Option[T] tagged-section path, keeping the wire compact for the
// common case where every field carries a real value.
var (
	MembProgressCurrent = KeelsonHrNkRegistry.MustBegin("progressCurrent").
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembProgressTotal = KeelsonHrNkRegistry.MustBegin("progressTotal").
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembProgressUnit = KeelsonHrNkRegistry.MustBegin("progressUnit").
				MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembProgressThroughputPerSec = KeelsonHrNkRegistry.MustBegin("progressThroughputPerSec").
					MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembProgressEtaMs = KeelsonHrNkRegistry.MustBegin("progressEtaMs").
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
