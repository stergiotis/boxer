// Package peaks holds the multi-resolution min/max pyramid the waveform
// player draws (ADR-0208 §SD2) and the sidecar cache file it is written to
// (§SD4, §SD12). It knows about samples and bins; it knows nothing about
// drawing, decoding or devices.
//
// # Data model
//
// A [Pyramid] summarises a [pcm.SourceI] of a known frame count. Level 0
// holds, per channel and per bin of BaseBin frames, the minimum and the
// maximum sample of that bin, quantised to int8 in [-127, 127]. Level k+1
// bin i is the min/max of level-k bins 2i and 2i+1, so a bin at level k
// spans BaseBin<<k frames; levels are added until the top one holds a
// single bin. Storage is struct-of-arrays: one []int8 of minima and one of
// maxima per (level, channel), all carved out of a single allocation sized
// at construction.
//
// # Containment
//
// Quantisation rounds outward, so a bin contains the samples it summarises
// rather than approximating them: with q(s) = clamp(s, -1, 1) * 127, a
// bin's minimum is min(floor(q(s))) and its maximum is max(ceil(q(s))) over
// its samples, and combining bins preserves that. For every sample s of
// every frame of a bin,
//
//	binMin <= floor(q(s)) <= ceil(q(s)) <= binMax
//
// holds at every level. A drawn column is therefore never narrower than the
// signal it stands for. NaN reads as zero; ±Inf clamps to full scale.
// -128 is never produced, which leaves the negation in [Pyramid.GlobalPeak]
// safe.
//
// # Concurrency
//
// One goroutine builds — [Pyramid.FoldE], [Pyramid.Finish],
// [Pyramid.FillFromE] — and any number of goroutines read
// [Pyramid.Query], [Pyramid.Columns] and the progress accessors
// concurrently with it, without a lock. What makes that sound: the level
// arrays are allocated once and never reallocated, the builder appends bins
// in frame order, and it publishes the built prefix with a single atomic
// store of the folded frame count. A reader loads that count first and
// touches only bins whose frames lie entirely inside it, so it never reads
// a bin the builder is still combining. After [Pyramid.Finish] the whole
// pyramid — including the trailing partial bins — is readable. Building
// after Finish is an error.
//
// # Cache file
//
// [Pyramid.WriteToE] serialises a complete pyramid and nothing else: an
// 80-byte little-endian header (magic "BXPK", format version, the
// caller-supplied [Identity], the pcm format, the frame count, the base bin
// and the level count) followed by the level arrays in level-major then
// channel-major order, minima before maxima, one byte per bin, no
// compression. Identity is opaque here — the caller computes it from the
// source file and this package stores it verbatim and compares it on load,
// so [ReadFromE] rejects a cache that belongs to a different file.
// Derived products other than peaks get their own file (§SD12).
package peaks
