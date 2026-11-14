// Package lanesink is the Go end of the audio lane (ADR-0372, proposed,
// §SD4–§SD6 and §SD9): a device whose samples are played somewhere else — by
// the viewer page of a remote session, or by the page around a browser tab —
// and reach it as messages.
//
// A [Device] is handed a function that sends one message toward the remote
// device and is given the messages that come back ([Device.Receive]). What
// carries them — a pipe to the headless host and its WebSocket, a tab
// worker's port — is the host's; this package does not know. The messages
// are in wire.go, and the same bytes travel on every carrier.
//
// # Flow
//
// The remote device pulls. It reports, per stream, how many frames it has
// played; the device here sends only what brings a playing stream back to
// its target ahead of that count. What is in flight is therefore bounded by
// what the remote device consumed, and its clock is the one playback
// follows: nothing is paced by wall time on this side, so the two clocks
// cannot drift apart. The target is the lane's latency. It grows when
// reports come slower than it allows for — a page whose timers the browser
// has slowed — and shrinks back when they speed up.
//
// # With nobody listening
//
// A remote device is absent until it announces itself, and suspended while
// the browser has not let it play. A [Sink] is then the device-less clock of
// package sink: playing advances the position by wall time and no frames are
// read. An announcement moves every open stream onto the device at its
// current position, state, rate and volume, and a device that goes away
// moves them back. An announcement also voids the streams of an earlier one,
// so a page that reconnects starts clean.
//
// # Rates and formats
//
// The remote device states one sample rate; every stream is resampled to it
// by its engine (package playout). Chunks are interleaved signed 16-bit,
// mono or stereo.
package lanesink
