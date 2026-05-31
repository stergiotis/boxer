//go:build llm_generated_opus47

// Package grantrequest is the leeway-coded wire form of the
// capability-grant request payload published on
// `runtime.cap.request`. First broker request/reply DTO to migrate
// off the buscodec fxamacker-cbor default onto the ADR-0042 SoA codec.
//
// Pattern: nested-struct flatten. The producer-side
// [capbroker.GrantRequest] embeds an `app.SubjectFilter` (Pattern /
// Reason / Direction / Sticky). The leeway codec is flat by
// design — one fact kind, one row per call. Rather than introduce
// nested-row encoding to codegen, the codec DTO carries the
// SubjectFilter fields as *peer* tagged columns; conversion happens
// at the codec boundary inside `capbroker.MarshalRequest` /
// `UnmarshalRequest`. The broker's Go API (and the policy contract
// that consumes `req.SubjectFilter`) stays unchanged.
//
// Vocabulary:
//
//   - [vdd.MembAppId] — shared with task.* DTOs.
//   - [vdd.MembReason] — shared with TaskCancel / TaskError.
//   - [vdd.MembCapFilterPattern] / [vdd.MembCapDirection] /
//     [vdd.MembCapFilterSticky] — narrow, the flattened SubjectFilter
//     columns.
//
// Wire shape vs the legacy capbroker.GrantRequest JSON form:
//
//   - The nested `SubjectFilter` field becomes four peer columns
//     (capFilterPattern / reason / capDirection / capFilterSticky).
//   - `Direction app.CapDirectionE` (uint8 enum) → `Direction string`
//     (the canonical String() form). Producers call
//     `filter.Direction.String()` at construction; consumers parse
//     back with `app.ParseCapDirection(...)` (added in this
//     migration).
//   - New `FactId uint64` plain `id` and `At time.Time` plain `ts`
//     (per the leeway grammar contract — every fact has both).
package grantrequest

import "time"

// GrantRequest is the flat wire form of a capability-grant request.
// The Go-level [capbroker.GrantRequest] keeps its nested-struct shape;
// this struct is the codec-side projection only.
type GrantRequest struct {
	_ struct{} `kind:"grantRequest"`

	// FactId is the per-row event id (currently zero from the
	// producer; awaits the per-handle sequencer follow-up flagged in
	// the TaskProgress migration).
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// AppId names the app the grant targets. M2.3 logs but does not
	// enforce a mismatch between AppId and Msg.Sender; M4 NKey-based
	// identity upgrades that to an enforcement boundary.
	AppId string `lw:"appId,stringArray"`

	// FilterPattern is app.SubjectFilter.Pattern.
	FilterPattern string `lw:"capFilterPattern,stringArray"`

	// FilterReason is app.SubjectFilter.Reason. Shares the cross-DTO
	// `reason` vocabulary entry — the same column type the audit /
	// task-cancel rows use.
	FilterReason string `lw:"reason,textArray"`

	// FilterDirection is app.CapDirectionE.String() — one of "pub",
	// "sub", "pub+sub", or "unspecified".
	FilterDirection string `lw:"capDirection,symbol"`

	// FilterSticky is app.SubjectFilter.Sticky.
	FilterSticky bool `lw:"capFilterSticky,bool"`
}
