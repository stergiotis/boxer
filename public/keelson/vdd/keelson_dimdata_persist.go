package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Persist narrow memberships — the reply DTO for the runtime.persist
// broker. Shared columns (reason) live in keelson_dimdata_shared.go.
//
// PersistReply.Error maps to shared `reason` rather than a narrow
// `persistError` term: the field carries a short failure rationale
// (the persist backend's error message), same semantic as
// TaskCancel.Reason / TaskError.Reason / WatchReply.Reason. Reusing
// `reason` joins persist failures into the cross-DTO error-rationale
// column with no extra vocabulary.
var (
	// MembPersistFound is true when a Get located the requested key.
	// Meaningless on Set / Delete replies (the producer leaves it
	// false and consumers ignore it).
	MembPersistFound = KeelsonHrNkRegistry.MustBegin("persistFound").
				MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembPersistValue carries the opaque application-defined bytes
	// returned by a successful Get. Empty when Found is false or
	// when the operation was not a Get. Uses the codec's scalar-blob
	// grammar (the same path TaskDone.Result lit up in the previous
	// migration).
	MembPersistValue = KeelsonHrNkRegistry.MustBegin("persistValue").
				MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
