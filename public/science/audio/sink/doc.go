// Package sink is the playback-transport contract of the audio subtree
// (ADR-0208 §SD6) together with its device-less implementation.
//
// A sink plays one [github.com/stergiotis/boxer/public/science/audio/pcm.SourceI]
// and answers where playback is. Position is a frame index into that source,
// so the widget's time bases (ADR-0208 §SD9) derive from it rather than the
// other way round; a caller that wants a duration converts through
// [github.com/stergiotis/boxer/public/science/audio/pcm.Format]. The
// transport is deliberately small — play, pause, seek, rate, volume,
// position — because that is the whole surface the player needs and the
// whole surface a device-backed implementation can honour cheaply.
//
// The position contract has two halves. [SinkI.Position] reports the
// *audible* frame, clamped to [0, Frames()], and while
// [SinkI.State] is [StatePlaying] consecutive calls advance without any
// further prompting: the widget polls it once per frame and gets a playhead
// that moves. Running into the end of the source stops the sink there —
// [SinkI.State] becomes [StateStopped], [SinkI.Ended] becomes true and
// Position stays at Frames() — and that transition is observable from the
// getters alone, with no callback to subscribe to.
//
// [Null] is the sink for tests and for headless scenes. It computes the
// position from a [ClockI] instead of from frames delivered to a device, so
// a [ManualClock] makes playback deterministic and instantaneous; it reads
// no samples and owns no goroutines. It is also the fallback on a host with
// no audio device, where the player shows a "no output device" state rather
// than failing to open one.
//
// [SinkI] is the authority seam as well. `track` is handed a sink and never
// opens a device itself, so an audio-output capability brokered by the
// keelson runtime — the shape of ADR-0026 §SD7's file-dialog powerbox, and
// what an ADR-0207 compartment boundary would enforce — can be introduced
// by handing out a different sink, without touching the player. The
// pulse-protocol sink that actually makes sound is ADR-0208 M3; this package
// carries no device dependency until then.
package sink
