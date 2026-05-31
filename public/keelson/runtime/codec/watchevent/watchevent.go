//go:build llm_generated_opus47

// Package watchevent is the leeway-coded wire form of one
// filesystem-watch event published on `fs.handle.{uuid}.event`.
//
// Vocabulary:
//
//   - [vdd.MembWatchEventKind] — narrow symbol; the
//     fsbroker.WatchEventKindE rendered as its canonical String()
//     ("create" / "delete" / "modify" / "attrib" / "renameFrom" /
//     "renameTo" / "overflow" / "closed" / "unspecified"). A new
//     fsbroker.ParseWatchEventKind helper provides the symmetric
//     inverse for read-side reconstruction.
//   - [vdd.MembWatchEventName] — narrow string; basename or relative
//     path. Empty when the event addresses the watched root itself.
//   - [vdd.MembWatchEventCookie] — narrow u32; inotify
//     RenameFrom/RenameTo pairing cookie. Zero on poller-backed
//     watches.
//
// The producer-side Ts field (unix nanoseconds — `watcher.go` uses
// `time.Now().UnixNano()` everywhere) maps to the codec's plain
// `ts=At` via `time.Unix(0, Ts)` at the boundary.
package watchevent

import "time"

// WatchEvent is the flat wire form of one filesystem event.
type WatchEvent struct {
	_ struct{} `kind:"watchEvent"`

	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// Kind is fsbroker.WatchEventKindE.String() — the canonical
	// rendering of the event class.
	Kind string `lw:"watchEventKind,symbol"`

	// Name is the affected entry's basename or relative path. Empty
	// when the event addresses the watched root.
	Name string `lw:"watchEventName,stringArray"`

	// Cookie pairs RenameFrom / RenameTo events on inotify-backed
	// watches; zero elsewhere.
	Cookie uint32 `lw:"watchEventCookie,u32Array"`
}
