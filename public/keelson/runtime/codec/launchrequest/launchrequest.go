// Package launchrequest is the leeway-coded wire form of the app-launch
// request payload published on `windowhost.open` (ADR-0135 §SD1). The
// request names a target app and optionally carries a launch config as
// facts-CBOR bytes produced by the target app's own generated codec
// (§SD2); the window host validates the claimed kind and the size cap at
// the boundary before any decode.
//
// Vocabulary:
//
//   - [vdd.MembAppId] — shared with the task.* / grant DTOs; here the
//     app the launch targets. The caller's identity is deliberately NOT
//     a payload field — the host attributes it from the bus envelope
//     (Msg.Sender) the way capbroker attributes grant requesters.
//   - [vdd.MembLaunchConfigKind] / [vdd.MembLaunchConfig] — narrow, the
//     claimed config kind and the config bytes.
package launchrequest

import "time"

// LaunchRequest is the flat wire form of an app-launch request.
type LaunchRequest struct {
	_ struct{} `kind:"launchRequest"`

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

	// TargetAppId names the app to open.
	TargetAppId string `lw:"appId,stringArray"`

	// ConfigKind is the vocabulary kind name Config's bytes claim
	// (e.g. "playLaunch"). Empty for a plain open. The host refuses a
	// mismatch against the target manifest's LaunchKind before any
	// decode reaches the app.
	ConfigKind string `lw:"launchConfigKind,symbol"`

	// Config is the launch config as facts-CBOR bytes produced by the
	// target app's generated codec. Empty means "open plainly". Capped
	// at the host boundary (64 KiB) before any decode.
	Config []byte `lw:"launchConfig,blobArray"`
}
