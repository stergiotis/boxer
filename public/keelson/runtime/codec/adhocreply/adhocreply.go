// Package adhocreply is the leeway-coded wire form of the ad-hoc dataset
// capability's answer to a request (ADR-0240 §SD8), sibling to
// [keelson/runtime/codec/adhocrequest]. One kind answers all three verbs:
// a refusal travels as Ok false with a Reason, never as a silent drop or a
// bare timeout. A publish reply carries the handle, revision, rows and
// bytes; a resolve reply adds the creation instant and the two liveness
// answers of ADR-0188 §SD3; a retract reply is Ok alone.
//
// Vocabulary: the `adhoc…` cohort in vdd and the shared `reason`.
package adhocreply

import "time"

// AdhocReply is the flat wire form of one reply.
type AdhocReply struct {
	_ struct{} `kind:"adhocReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the reply instant.
	At time.Time `lw:",ts"`

	// Ok is true when the verb applied. False with Reason otherwise.
	Ok bool `lw:"adhocReplyOk,bool"`

	// Reason carries the refusal or failure rationale; empty when Ok.
	Reason string `lw:"reason,textArray"`

	// Handle is the dataset the verb concerned: minted or reused by a
	// publish, found by a resolve.
	Handle string `lw:"adhocHandle,stringArray"`

	// Revision, Rows and Bytes describe that dataset.
	Revision uint64 `lw:"adhocRevision,u64Array"`
	Rows     uint64 `lw:"adhocRows,u64Array"`
	Bytes    uint64 `lw:"adhocBytes,u64Array"`

	// CreatedAtUs is the dataset's creation instant, unix µs (resolve).
	CreatedAtUs int64 `lw:"adhocCreatedAtUs,i64Array"`

	// HandleLive answers the bound handle a resolve asked about: still
	// live, or left. Meaningful whether or not the alias itself resolved.
	HandleLive bool `lw:"adhocHandleLive,bool"`

	// NoLive marks a failed resolve as "nothing live under the alias"
	// rather than a malformed request, so the caller waits, not retries.
	NoLive bool `lw:"adhocNoLive,bool"`

	// Bundle, Document, LocalNames and Handles answer a bundle verb: the
	// bundle's alias, its applet document (resolve), and its datasets'
	// local names with their handles, index for index. Revision is then
	// the bundle's revision.
	Bundle     string   `lw:"adhocBundle,symbol"`
	Document   []byte   `lw:"adhocDocument,blobArray"`
	LocalNames []string `lw:"adhocLocalNames,stringArray"`
	Handles    []string `lw:"adhocHandles,stringArray"`

	// ArrowStream and StreamDigest answer `adhoc.read` (ADR-0288 (proposed)
	// §SD6): the dataset's stream as sealed, and its content digest.
	ArrowStream  []byte `lw:"adhocArrowStream,blobArray"`
	StreamDigest string `lw:"adhocStreamDigest,stringArray"`

	// Alias is the alias a publish went under: the window's own when the
	// request was window-scoped (ADR-0288 (proposed) §SD3).
	Alias string `lw:"adhocAlias,symbol"`

	// Destination is the grant destination a refused read needed
	// (ADR-0288 (proposed) §SD6); empty otherwise.
	Destination string `lw:"adhocDestination,stringArray"`
}
