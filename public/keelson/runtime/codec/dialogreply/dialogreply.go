// Package dialogreply is the leeway-coded wire form of the
// file-dialog reply payload published on `fs.dialog.{op}` replies.
//
// Vocabulary:
//
//   - [vdd.MembDialogApproved] — narrow boolean (kept narrow vs.
//     elevating to a shared `approved` term — see the fsbroker vdd
//     file's rationale).
//   - [vdd.MembDialogHandleSubject] — narrow string; the NATS subject
//     prefix the caller's caps are scoped to on approval.
//   - [vdd.MembReason] — shared, populated on denial and broker-side
//     errors.
//
// The Go-level [fsbroker.DialogReply] keeps its existing shape
// (Granted/HandleSubjectPrefix/Reason); this struct is the
// codec-side projection only. Conversion lives in
// `fsbroker.MarshalDialogReply` / `UnmarshalDialogReply`. New plain
// columns (FactId/At) are stamped at marshal time inside those
// helpers; the broker's existing API is unchanged.
package dialogreply

import "time"

// DialogReply is the flat wire form of a file-dialog reply.
type DialogReply struct {
	_ struct{} `kind:"dialogReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// Approved carries the user's accept/cancel decision (false also
	// covers broker-side errors with Reason populated).
	Approved bool `lw:"dialogApproved,bool"`

	// HandleSubjectPrefix is the NATS subject prefix the caller's
	// caps cover on approval. Empty on denial.
	HandleSubjectPrefix string `lw:"dialogHandleSubject,stringArray"`

	// Reason is the rationale (denial reason or broker error). Empty
	// on a successful approval.
	Reason string `lw:"reason,textArray"`
}
