// Package pcm is the sample-level contract of the audio subtree (ADR-0208
// SD5): a [Format], the positioned frame reader [SourceI] that every decoder
// implements and every consumer reads through, and two in-memory sources for
// tests, demos and benchmarks.
//
// Samples are interleaved float32 frames in [-1, 1]; a frame is one sample
// per channel. Positions are frame indices, never byte offsets or durations —
// the widget's time bases (ADR-0208 SD9) derive from frames, not the other
// way round.
//
// The read contract is [SourceI.ReadFramesAtE]; a decoder is allowed to be
// expensive on a backwards seek (an ffmpeg-backed source restarts the
// process), so callers that need random access cache windows above this
// interface rather than expecting it to be cheap. Sequential callers — the
// peaks builder — read increasing offsets and a decoder may optimise for
// that.
//
// [pcmtest.CheckSourceContract] under the pcmtest subpackage verifies an
// implementation against the contract; every decoder's tests should run it.
package pcm
