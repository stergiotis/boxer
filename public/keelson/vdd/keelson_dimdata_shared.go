//go:build llm_generated_opus47

package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Abstract / shared memberships — these intentionally live outside any
// single fact kind so multiple DTOs can reuse the same vocabulary entry.
// Sharing the same membership across kinds means a query like "all rows
// with a Note" matches every kind that carries one. This is the
// cross-cutting half of the broker-DTO-as-fact migration (the kind-narrow
// half lives in keelson_dimdata_<kind>.go siblings).
//
// Adding a new shared membership: only do so when the semantics are
// genuinely cross-cutting (taskId, note, reason). Kind-specific fields
// stay narrow and prefixed (cgSubject, progressCurrent, errorMsg).
var (
	// MembTaskId is the per-task identifier carried by every wire DTO in
	// the task package (TaskCreated/Progress/Done/Error/Cancel). String
	// section: the producer-supplied TaskIdT is opaque to the codec.
	MembTaskId = KeelsonHrNkRegistry.MustBegin("taskId").
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembNote is a free-text annotation column reusable by any DTO that
	// carries a short human-readable hint alongside its structured payload
	// (TaskProgress, errkind.Error, future audit DTOs). Text section
	// because notes are high-cardinality prose, not a dictionary symbol.
	MembNote = KeelsonHrNkRegistry.MustBegin("note").
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembTitle is a short human-readable label column reusable by any
	// DTO that carries a one-line title alongside its structured payload
	// (TaskCreated, future audit / event DTOs). Text section mirrors
	// MembNote — both are short prose, not dictionary symbols.
	MembTitle = KeelsonHrNkRegistry.MustBegin("title").
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembAppId names the owning runtime app for any DTO bound to an
	// app-instance lifecycle. String section because app.AppIdT is
	// opaque to the codec (namespaced like "test.app"); a future
	// LowCardinality refactor is a one-line section flip if the
	// observed cardinality stays small.
	MembAppId = KeelsonHrNkRegistry.MustBegin("appId").
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembTileKey is the window/tile identifier inside the host runtime
	// (used for audit join-back to lifecycle facts). u64 section; 0 is
	// the "no tile context" sentinel.
	MembTileKey = KeelsonHrNkRegistry.MustBegin("tileKey").
			MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembRunId is the runtime-start identifier (nanoid/uuid) so audit
	// rows can join back to runtime-lifecycle facts. String section
	// because the id is opaque and high-cardinality across restarts.
	MembRunId = KeelsonHrNkRegistry.MustBegin("runId").
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembReason is a free-text rationale column reusable by any DTO
	// that carries a "why this happened" annotation (TaskCancel,
	// TaskError, future audit DTOs). Text section because reasons are
	// short prose, not a dictionary symbol — parallels MembNote and
	// MembTitle.
	MembReason = KeelsonHrNkRegistry.MustBegin("reason").
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

	// MembErrorText is the rendered error chain (today: the
	// FormatErrorWithStackS multi-line text; a future structured-CBOR
	// variant lands as a separate column when consumers can decode it).
	// Reusable by any DTO that surfaces a captured error alongside its
	// payload — TaskError today, future RPC-reply DTOs that carry a
	// non-nil error.
	MembErrorText = KeelsonHrNkRegistry.MustBegin("errorText").
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
