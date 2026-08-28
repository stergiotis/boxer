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
// out of [OpenE] closes the source before returning. A caller therefore
// never has to reason about whether a failed open left a file descriptor
// behind, and must not close the source it passed in.
//
// The sink is handed in as well, by [Options.NewSink] — this package never
// opens an audio device (ADR-0208 §SD6), which is what leaves room for a
// brokered audio-output capability to supply a different sink without the
// player changing.
//
// # The locked-source seam
//
// A [pcm.SourceI] is safe for one goroutine at a time, and a track has more
// than one reader: the widget's frame thread calls [Track.ReadWindowE] while
// a device-backed sink's callback goroutine (ADR-0208 M3) reads the same
// source to fill its buffer. The source is therefore wrapped once, in
// [OpenE], in an unexported adapter that serialises every read and the close
// behind a mutex, and it is that adapter — not the raw source — that the
// peaks build, [Track.ReadWindowE] and [Options.NewSink] all see. Format and
// frame count are immutable and answered without the lock. Serialising here
// rather than in each caller is what makes "safe to call from any goroutine"
// true of the whole surface, and it means the M3 sink needs no locking of its
// own.
//
// # Two time bases
//
// The position is a frame index (ADR-0208 §SD9). A [TimeBase] of a sample
// rate plus an optional epoch — the wall-clock instant of frame 0 — turns it
// into what is shown: with no epoch every readout is a duration from the
// start of the recording, and with one the same frame reads as an instant.
// Both readings come from the same frame, so a caller may offer the user a
// switch between them without moving the view.
//
// # What M4 adds
//
// ADR-0208 §SD4 builds the pyramid in the background and §SD3 puts a
// byte-bounded window cache in front of the raw reads. Neither changes the
// surface here: the build's progress is already published by the pyramid
// ([peaks.Pyramid.Built], [peaks.Pyramid.IsComplete]) rather than by
// [OpenE] returning, so a background build is a new [Options] field and an
// [OpenE] that returns earlier, and a window cache sits inside
// [Track.ReadWindowE]. This milestone builds synchronously, so a track is
// complete when [OpenE] returns, and a raw window is a direct read through
// the shared source.
package track
