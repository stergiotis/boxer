// Package watchreply is the leeway-coded wire form of the
// watch-reply payload on `fs.handle.{uuid}.watch`.
//
// Vocabulary:
//
//   - [vdd.MembWatchStarted] — narrow bool. False either on a
//     broker-side error (Reason populated) or when the handle
//     already has an active watch.
//   - [vdd.MembWatchEventSubject] — narrow string; the subject
//     events publish to on success.
//   - [vdd.MembWatchBackend] — narrow symbol; the watcher
//     implementation that was selected ("inotify" / "poller").
//   - [vdd.MembReason] — shared, populated on failure.
package watchreply

import "time"

// WatchReply is the flat wire form of a watch reply.
type WatchReply struct {
	_ struct{} `kind:"watchReply"`

	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// Started signals whether the watch actually started.
	Started bool `lw:"watchStarted,bool"`

	// EventSubject is the subject events publish to on success.
	EventSubject string `lw:"watchEventSubject,stringArray"`

	// Backend names the watcher implementation that was selected.
	Backend string `lw:"watchBackend,symbol"`

	// Reason carries the failure rationale (empty on success).
	Reason string `lw:"reason,textArray"`
}
