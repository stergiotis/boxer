// Package pulsesink is the device sink of ADR-0208 SD6: a [sink.SinkI] that
// plays a [pcm.SourceI] through the PulseAudio native protocol in pure Go
// (jfreymuth/pulse), which is what a PipeWire or PulseAudio desktop serves.
// It lives in its own package so sink's contract and its device-less Null
// stay dependency-free.
//
// # Stream
//
// The stream is opened at the source's own sample rate and channel count; the
// server resamples to the device, so the sink never resamples for a rate
// mismatch. A playback rate other than 1 is a linear-interpolation resample
// of the source in the pull callback — pitch follows rate, as SD6 decided —
// and volume is a software gain applied in the same callback. The callback
// reads the source strictly forwards at every rate: the frame or two of
// lookahead the interpolation needs are kept across calls rather than read
// again, because a decoder whose random access is a process restart (SD5)
// would otherwise restart on every callback — a stutter that, since the
// fractional position survives a return to rate 1, would never end. Back at
// exactly rate 1 the fraction is dropped and playback is bit-exact again.
//
// # Position
//
// The server pulls; the callback counts the source frames it hands over. The
// audible position is that count, minus the frames still in the server's
// buffer, interpolated by wall clock between callbacks and never allowed to
// run backwards or ahead of what was delivered. Against the SD6 contract this
// is the "frames delivered minus buffer" estimate; the protocol's latency
// query is the recorded correction if it ever proves off.
//
// # Transport
//
// The stream is started once, by the first Play, and then left running and
// uncorked for as long as the sink is open. Everything else is done to the
// pull callback: while the sink is paused, stopped or at the end of its
// source the callback waits, Play releases it, and a seek moves the cursor
// it reads from. What the server already holds — one latency at most — plays
// out after a pause, and after a seek while playing it is still the old
// position that is heard for that long.
//
// The stream is left alone because the library (jfreymuth/pulse v0.1.3)
// offers no call on it that is safe to repeat. An uncork in place makes the
// server send a Started event, which the library answers with a blocking
// send that only its Start receives, so a Resume parks its read loop for
// good and every later round trip times out (upstream issue #52). And Start
// waits without a time limit for that event, which the server sends only
// when the stream had run dry before and now holds a buffer of frames: a
// second Start right after a cork, or over a source that has less than a
// buffer to give, waits for good. oto's PulseAudio backend suspends by
// waiting in the callback too.
//
// So that no caller waits, the one Start runs on a goroutine of its own, and
// for the first two buffers after it the callback delivers frames whatever
// the transport is doing — silence where the sink is paused or the source
// has ended or failed — which is what lets the server report the start.
//
// Three consequences. The server runs dry whenever the callback waits, so
// Underflow keeps the value it had when playback was first paused or first
// ended. While the callback waits the library processes no server events, so
// nothing here makes a round trip then. And the stream does not end with the
// source: Play at the end plays from frame 0 again, as the Null sink does,
// on the same stream.
//
// # Channels
//
// Mono and stereo sources are played as they are; more channels are refused
// at open (a channel map for them is deferred until a source needs it).
package pulsesink
