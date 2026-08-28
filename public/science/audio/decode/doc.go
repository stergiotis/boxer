// Package decode opens a recording as a [pcm.SourceI] without the caller
// naming its format (ADR-0208 §SD5): one interface, two decoders, chosen by
// the file's first bytes. It also computes the peaks-cache identity of a file
// (§SD4), which is the one other thing a consumer needs before it can start a
// build.
//
// # Sniffing
//
// [Sniff] looks at the first twelve bytes. A RIFF, RF64 or BW64 container
// whose form type is WAVE is decoded by the native reader in the wavfile
// package — no process, equally cheap sequential and random access, and RF64
// so a twelve-hour recording past the 4 GB RIFF limit opens. Everything else
// goes to ffmpeg, including bytes that resemble nothing: sniffing decides
// which decoder gets the file, not whether the file is valid, and ffmpeg's
// own probe is a better judge of the long tail of containers than a magic
// table maintained here would be. Fewer than twelve bytes is
// [KindUnknown] — too little to tell — and [OpenE] hands those to ffmpeg
// too, so the error a reader sees names the format, not the sniff.
//
// # The declared length is the length
//
// [pcm.SourceI] requires the frame count up front, and for a compressed file
// that count comes from ffprobe as round(duration × sample rate). A codec
// then decodes to a slightly different number of frames than its container's
// duration implies — a Vorbis stream's granule positions, an MP3's encoder
// delay. [FfmpegSource] therefore honours the declared count exactly: frames
// ffmpeg does not deliver read as silence and are counted by
// [FfmpegSource.Padded], and frames past the declared end are never
// requested. Consumers depend on this — the peaks builder preallocates every
// pyramid level from Frames() before the first read (§SD4), and a source that
// ran a few frames short would leave the pyramid permanently incomplete.
// [FfmpegSource.DeclaredFrames] is the probe's own count for a caller that
// wants to see it.
//
// # Restarts, and reopening instead
//
// The ffmpeg process is a stream: it delivers frames from where it was
// started, in order. A read at the position the process has reached continues
// that stream, which is what the sequential peaks build and the playback sink
// do. A read anywhere else kills the process and starts a new one at the new
// offset — one process spawn plus ffmpeg's own decode-and-discard up to the
// seek point. [FfmpegSource.Restarts] counts those, so a consumer whose
// access pattern is thrashing can be seen doing it rather than merely felt.
//
// The answer to two consumers with two positions is two decoders, not a
// smarter one: [Reopener] returns a function that opens another independent
// source over the same recording, and a track gives its peaks builder, its
// sink and its window cache one each (§SD3 caches windows above this
// interface for exactly this reason).
//
// # Identity
//
// [IdentityE] fingerprints a file for the peaks cache: its size, its
// modification time, and a blake3 over those two plus its first and last
// 1 MiB — at most 2 MiB of reads however long the recording. It is a
// fingerprint, not a checksum. A file re-encoded to the same length, or
// edited in the middle and restored to its original size and mtime, has the
// same identity and would be drawn with a stale pyramid. That is the trade
// §SD4 makes: hashing twelve hours of audio to open a cache would cost more
// than rebuilding the pyramid the cache exists to avoid.
package decode
