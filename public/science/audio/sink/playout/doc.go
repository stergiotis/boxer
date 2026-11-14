// Package playout is the part of a device-backed sink that is not the
// device (ADR-0372 §SD3, proposed): the transport state, the cursor into the
// source, the rate resampler, the gain and the position bookkeeping of
// ADR-0208 §SD6, over any device that takes interleaved float32 frames.
//
// An [Engine] has two halves. The methods of
// [github.com/stergiotis/boxer/public/science/audio/sink.SinkI] are what a
// player calls, from any goroutine. The methods ending in Locked are what a
// device calls, between [Engine.Lock] and [Engine.Unlock]: it asks for
// frames with [Engine.ProduceLocked] while the engine is playing, ends
// playback with [Engine.EndLocked] when that returns none, and says how many
// frames it holds unplayed with [Engine.SetLagLocked], after every call that
// wrote frames, or which source frames with [Engine.SetLagSourceLocked].
// Between Lock and Unlock only the Locked methods may be called: the others
// take the lock themselves. A device that owns
// further state of its own keeps it under the same lock, so a transport call
// and a pull never see each other half done. A device type wraps an engine
// and adds what only it does to a transport call — starting a stream on the
// first Play, dropping a remote buffer on a seek.
//
// # Reads
//
// The source read happens under the engine's lock, as it did in the pulse
// sink: a transport call made while a read is under way waits for it.
//
// The source is read strictly forwards at every rate: the frame or two of
// lookahead the interpolation needs are kept across calls rather than read
// again, because a decoder whose random access is a process restart
// (ADR-0208 §SD5) would otherwise restart on every pull. At a step of exactly
// one source frame per device frame the fraction is dropped and the frames
// pass through unchanged.
//
// # Rates
//
// A device either resamples itself, and is given the source's rate, or
// states the one rate it takes ([Options.DeviceRate]). The engine's linear
// interpolation then covers the playback rate and the ratio of the two
// sample rates in one step. Pitch follows the playback rate.
//
// # Position
//
// The engine counts the source frames it hands over. The audible position
// is that count, minus what the device holds unplayed, interpolated by wall
// clock since the last anchor and never allowed to run backwards or ahead of
// what was delivered.
package playout
