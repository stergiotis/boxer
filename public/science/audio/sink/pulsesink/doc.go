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
// Pause corks the stream and keeps its buffer, so Resume continues without a
// gap. A seek corks, drops the stream to idle and starts it again, which
// flushes the server buffer — anything else would play the old position for
// a buffer's worth of time. The end of the source ends the stream; Play then
// restarts from frame 0, as the Null sink does.
//
// # Channels
//
// Mono and stereo sources are played as they are; more channels are refused
// at open (a channel map for them is deferred until a source needs it).
package pulsesink
