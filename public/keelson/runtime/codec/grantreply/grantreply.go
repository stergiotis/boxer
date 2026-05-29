//go:build llm_generated_opus47

// Package grantreply is the leeway-coded wire form of the
// capability-grant reply payload. Sibling to
// [keelson/runtime/codec/grantrequest]; both opened the broker
// request/reply DTO cohort.
//
// Vocabulary:
//
//   - [vdd.MembGrantApproved] — narrow, the policy decision (false ⇒
//     denied).
//   - [vdd.MembGrantId] — narrow, the broker-local grant identifier on
//     approval. Empty string on denial.
//   - [vdd.MembReason] — shared, rationale either way (policy text on
//     approval; error or denial reason on rejection).
//
// Wire shape vs the legacy capbroker.GrantReply JSON form:
//
//   - `Granted` → `Approved` (the wire vocabulary names it
//     `grantApproved`; the Go field follows). Same boolean semantics.
//   - New `FactId uint64` plain `id` and `AtNs int64` plain `ts`.
package grantreply

// GrantReply is the wire form of a capability-grant reply. The
// Go-level [capbroker.GrantReply] keeps its existing shape
// (Granted/GrantId/Reason); this struct is the codec-side projection
// only.
type GrantReply struct {
	_ struct{} `kind:"grantReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// AtNs is the reply timestamp in unix nanoseconds; emitted as
	// u32 seconds on the wire.
	AtNs int64 `lw:",ts"`

	// Approved carries the policy decision.
	Approved bool `lw:"grantApproved,bool"`

	// GrantId is the broker-local identifier on approval (decimal
	// uint64-as-string). Empty on denial.
	GrantId string `lw:"grantId,stringArray"`

	// Reason is the rationale (policy text on approval; error
	// message on denial). Shares the cross-DTO `reason` vocabulary
	// entry.
	Reason string `lw:"reason,textArray"`
}
