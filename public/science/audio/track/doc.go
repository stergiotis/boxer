// Package track composes one open recording out of the layers below it
// (ADR-0208 §SD1): a [github.com/stergiotis/boxer/public/science/audio/pcm.SourceI]
// to read frames from, a [github.com/stergiotis/boxer/public/science/audio/peaks.Pyramid]
// to draw, a [github.com/stergiotis/boxer/public/science/audio/sink.SinkI]
// to play through, and the [TimeBase] every shown time derives from. It is
// the media element the waveform widget holds; the widget imports this
// package and nothing beneath it, and this package imports nothing from
// imzero2.
//
// # Ownership
//
// [OpenE] is handed an already-open source and takes ownership of it:
// [Track.CloseE] closes the sink and then the source, and every error path
// out of [OpenE] closes the source — and anything [Options.Reopen] opened —
// before returning. A caller therefore never has to reason about whether a
// failed open left a file descriptor behind, and must not close the source it
// passed in.
//
// The sink is handed in as well, by [Options.NewSink] — this package never
// opens an audio device (ADR-0208 §SD6), which is what leaves room for a
// brokered audio-output capability to supply a different sink without the
// player changing.
//
// # Three readers, one recording
//
// A track has three readers with unrelated access patterns: the peaks build
// walks the recording once from front to back, the window cache jumps to
// wherever the view is zoomed in, and the sink reads forward from the
// playhead. A [pcm.SourceI] is safe for one goroutine at a time, so the
// source [OpenE] is given is wrapped once in an unexported adapter that
// serialises every read and the close behind a mutex; format and frame count
// are immutable and answered without the lock, since the frame thread asks
// for them per rendered frame and must not queue behind a decoder.
//
// Serialising is enough when a seek is cheap — a memory source, a WAV file.
// It is the wrong answer for a decoder whose random access is a process
// restart (ffmpeg, ADR-0208 §SD5), where three interleaved readers would
// restart it against each other indefinitely. [Options.Reopen] is the seam
// for that case: it opens an independent source over the same recording, and
// the build and the window cache each get one, so each keeps its own file
// position. The sink keeps the source [OpenE] was given. The build's source
// is closed as soon as the build ends; the window cache's lives until
// [Track.CloseE].
//
// # The build and its progress
//
// The pyramid is preallocated before [OpenE] returns, so [Track.Peaks] is
// never nil, and it is filled in one sequential pass — synchronously by
// default, or on a goroutine of its own under [Options.Background]
// (ADR-0208 §SD4). A background build publishes the built prefix as one
// atomic frame count, which readers on the frame thread draw without a lock;
// [Track.BuildProgress] is that count plus whether the build finished, came
// from the cache, or failed. [Options.Progress] runs on whichever goroutine
// is building, so a callback that touches UI state has to hand it across
// itself.
//
// A background build's lifetime is the track's, not the open call's:
// [Track.CloseE] cancels it and waits for the goroutine to leave the source
// alone before closing anything.
//
// # The peaks cache
//
// With an [Options.Identity] — a hash of the recording's size, modification
// time and head/tail bytes — [OpenE] first looks for a finished pyramid in
// the cache directory ([Options.CacheDir], else [ResolvePeaksCacheDir], which
// reads [PeaksCacheDir]). The file is `<hex of the identity hash's first 16
// bytes>-b<base bin>` plus [CacheFileExt], written through a temporary file
// in the same directory and renamed into place, so a reader finds either the
// previous file or the whole new one.
//
// Every way of not getting a pyramid out of that file — no file, another
// recording's identity, a truncated body — is a miss, logged at debug and
// built over. A cache that cannot be written is likewise not a failed build:
// it costs the next open its head start, and is reported apart from the build
// in [BuildProgress.CacheErr]. §SD12 keeps other derived products out of this
// file: a spectrogram's cache is its own file in the same directory under the
// same identity.
//
// # Windows for the deepest zoom
//
// Below the base bin there are no peaks to draw and the raw frames are what
// the view needs (ADR-0208 §SD3). [Track.ReadWindowE] reads them
// synchronously, which is what a batch job wants; the frame thread uses
// [Track.Window], whose contract is the portolan tile one:
//
//   - A cached window comes back with ok=true. Anything else schedules a
//     fetch and returns ok=false, meaning "draw the pyramid and ask again
//     next frame" — never "there is no audio here".
//   - The returned slice is read-only and stays valid for as long as the
//     caller holds it. An evicted window is released to the garbage
//     collector, never reused for another fetch.
//   - Requests are capped ([MaxWindowFrames]) and the cache is bounded by
//     bytes ([Options.WindowCacheBytes]); a request that cannot fit is
//     refused with ok=false however often it is repeated.
//   - One fetch runs at a time, and a queued request that has not started is
//     replaced by the next one — a view being zoomed or panned supersedes its
//     own requests faster than a decoder can serve them.
//
// # Two time bases
//
// The position is a frame index (ADR-0208 §SD9). A [TimeBase] of a sample
// rate plus an optional epoch — the wall-clock instant of frame 0 — turns it
// into what is shown: with no epoch every readout is a duration from the
// start of the recording, and with one the same frame reads as an instant.
// Both readings come from the same frame, so a caller may offer the user a
// switch between them without moving the view.
package track
