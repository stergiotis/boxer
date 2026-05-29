//go:build llm_generated_opus47

// Package watchrequest is the leeway-coded wire form of the
// watch-request payload on `fs.handle.{uuid}.watch`.
//
// Vocabulary: three narrow terms ([vdd.MembWatchPollFallback],
// [vdd.MembWatchPollIntervalMs], [vdd.MembWatchRecursive]). All
// fields default to zero values; the broker-side defaults
// (auto-routing inotify/poller, 500ms tick, single-level watch)
// kick in when the wire carries zeros.
//
// The legacy "empty payload yields zero WatchRequest" interop hook
// in `fsbroker.UnmarshalWatchRequest` survives the migration —
// callers that publish nil to use defaults stay wire-compatible.
package watchrequest

// WatchRequest is the flat wire form of a watch request.
type WatchRequest struct {
	_ struct{} `kind:"watchRequest"`

	FactId uint64 `lw:",id"`

	// AtNs is the request timestamp; stamped at marshal time.
	AtNs int64 `lw:",ts"`

	// PollFallback forces the poller backend regardless of the
	// underlying filesystem.
	PollFallback bool `lw:"watchPollFallback,bool"`

	// PollIntervalMs is the poller tick interval. Zero selects
	// default 500ms; values below 100ms clamp at the broker.
	PollIntervalMs int32 `lw:"watchPollIntervalMs,i32Array"`

	// Recursive enables subtree watching from the handle's root.
	Recursive bool `lw:"watchRecursive,bool"`
}
