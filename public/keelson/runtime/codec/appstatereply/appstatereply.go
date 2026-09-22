// Package appstatereply is the leeway-coded wire form of the app-state
// service's answer (ADR-0185 §SD3), sibling to
// [keelson/runtime/codec/appstaterequest]. A refusal travels as a reply with
// Ok false and a Reason, never as a silent drop or a bare timeout.
//
// The outcome comes back per kind, as parallel columns: a delete answers
// with one entry, a forget with one per kind it found. A forget attempts
// every entry and never stops at the first failure (ADR-0185 §SD4), so a
// partial clear is reported as exactly what it was.
//
// Vocabulary: the `asOutcome…` cohort in vdd, Arbitrary cardinality, and
// the shared `reason`.
package appstatereply

import "time"

// AppStateReply is the flat wire form of one reply.
type AppStateReply struct {
	_ struct{} `kind:"appStateReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the reply instant.
	At time.Time `lw:",ts"`

	// Ok is true when every entry the verb attempted was cleared. False
	// with Reason otherwise — including a refusal before anything was
	// attempted.
	Ok bool `lw:"asReplyOk,bool"`

	// Reason carries the refusal or failure rationale; empty when Ok.
	Reason string `lw:"reason,textArray"`

	// The outcome per kind, zipped by index: entries cleared and entries
	// whose delete failed.
	Kind    []string `lw:"asOutcomeKind,symbolArray"`
	Cleared []uint64 `lw:"asOutcomeCleared,u64Array"`
	Failed  []uint64 `lw:"asOutcomeFailed,u64Array"`
}
