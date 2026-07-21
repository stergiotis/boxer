// Package launchreply is the leeway-coded wire form of the app-launch
// reply payload. Sibling to [keelson/runtime/codec/launchrequest]
// (ADR-0135 §SD1). Refusals travel as a reply with a Reason, never as a
// silent drop or a bare timeout.
//
// Vocabulary:
//
//   - [vdd.MembTileKey] — shared; the opened window's key. The window
//     host writes the same value as TileKey on the app-lifecycle
//     "started" row, so a launch reply joins its lifecycle row on one
//     column.
//   - [vdd.MembReason] — shared; empty on success, the refusal or
//     failure rationale otherwise (unknown app, kind mismatch,
//     oversize, malformed envelope, …).
package launchreply

import "time"

// LaunchReply is the flat wire form of an app-launch reply.
type LaunchReply struct {
	_ struct{} `kind:"launchReply"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// WindowKey is the key of the window the open created. Zero when
	// the open was refused. Shares the `tileKey` vocabulary term with
	// the app-lifecycle rows (the host records the same value there),
	// so launches join their lifecycle row directly.
	WindowKey uint64 `lw:"tileKey,u64Array"`

	// Reason carries the refusal or failure rationale. Empty on
	// success.
	Reason string `lw:"reason,textArray"`
}
