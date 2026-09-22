// Package keelsonqueryreply is the leeway-coded wire form of the
// introspection query service's answer (ADR-0253 §SD2), sibling to
// [keelson/runtime/codec/keelsonqueryrequest]. A refusal travels as a reply
// with Ok false and a Reason, never as a silent drop or a bare timeout.
//
// The body is the engine's bytes in the FORMAT the request named — rows are
// not decoded here, so the wire carries a reader's format (JSONEachRow) and
// an Arrow consumer's (ArrowStream) alike.
//
// Vocabulary: the `kqReply…` cohort in vdd, and the shared `reason`.
package keelsonqueryreply

import "time"

// KeelsonQueryReply is the flat wire form of one reply.
type KeelsonQueryReply struct {
	_ struct{} `kind:"keelsonQueryReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the reply instant.
	At time.Time `lw:",ts"`

	// Ok is true when the statement ran and Body is its result. False with
	// Reason otherwise — a refusal by the service or a failure in the
	// engine.
	Ok bool `lw:"kqReplyOk,bool"`

	// Reason carries the refusal or failure rationale; empty when Ok.
	Reason string `lw:"reason,textArray"`

	// ContentType is what the engine reported for Body.
	ContentType string `lw:"kqReplyContentType,stringArray"`

	// Body is the result in the request's FORMAT; empty on a refusal.
	Body []byte `lw:"kqReplyBody,blobArray"`
}
