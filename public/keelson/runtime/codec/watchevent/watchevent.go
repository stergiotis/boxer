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
// The producer-side Ts field maps directly to the codec's plain
// `ts=AtNs` (already unix nanoseconds — `watcher.go` uses
// `time.Now().UnixNano()` everywhere); no unit conversion needed,
// unlike the task.* migrations.
package watchevent

// WatchEvent is the flat wire form of one filesystem event.
type WatchEvent struct {
	_ struct{} `kind:"watchEvent"`

	FactId uint64 `lw:",id"`

	// AtNs is the event capture timestamp; producer-side already
	// unix nanoseconds (no ms → ns conversion needed).
	AtNs int64 `lw:",ts"`

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
