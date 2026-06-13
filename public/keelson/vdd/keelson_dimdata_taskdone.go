package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// TaskDone narrow memberships — the success-specific half of the
// keelson/runtime/codec/taskdone DTO. Shared columns (taskId) live in
// keelson_dimdata_shared.go.
//
// Result is opaque application-defined binary (decoded by observers
// based on the originating TaskCreated.Kind). The blob section is the
// canonical home for variable-length bytes that aren't text — this is
// the first in-tree consumer of the codec's scalar-blob grammar
// extension (sized integers spell themselves `[]uint8` and stay in
// the slice-of-u8 lane; `[]byte` is reserved for blob).
var (
	MembTaskResult = KeelsonHrNkRegistry.MustBegin("taskResult").
			MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
