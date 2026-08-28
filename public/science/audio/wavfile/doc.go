// Package wavfile decodes RIFF/WAVE and RF64/BW64 streams into interleaved
// float32 frames behind pcm.SourceI, and writes them back out (ADR-0208 §SD1
// for the home, §SD5 for the decoder's place among the two decoders).
//
// # What it reads
//
// WAVE_FORMAT_PCM at 8, 16, 24 and 32 bits — 8-bit samples unsigned, wider
// depths two's-complement little-endian — WAVE_FORMAT_IEEE_FLOAT at 32 and
// 64 bits, and WAVE_FORMAT_EXTENSIBLE whose SubFormat GUID names one of those
// two. wValidBitsPerSample is validated against the container it sits in;
// the extensible specification left-justifies a narrower sample inside its
// container, so the container's range is the conversion scale either way.
//
// The chunk walk tolerates chunks it does not know, the pad byte that follows
// an odd-sized chunk, and metadata (LIST, bext, …) on either side of fmt.
// The first data chunk is the one that counts. Every other format tag —
// ADPCM, A-law and µ-law, an MPEG payload in a WAVE wrapper — is rejected
// with the tag named in the error; those files reach the player through the
// ffmpeg decoder instead.
//
// # RF64
//
// A twelve-hour stereo 16-bit recording is 8.3 GB, so the 32-bit size fields
// of RIFF are not enough and EBU Tech 3306's RF64 (and the BW64 spelling of
// the same container) is part of the contract rather than an extra. The outer
// four-CC is RF64 or BW64, its 32-bit size field is the 0xFFFFFFFF escape,
// and a ds64 chunk carries the real 64-bit riffSize, dataSize and
// sampleCount plus a table of sizes for any other chunk that needs one. A
// chunk whose 32-bit size reads 0xFFFFFFFF takes its size from ds64: the
// data chunk from dataSize, anything else from the table. ds64's sampleCount
// is not what the frame count comes from — see below.
//
// # Truncation
//
// The frame count is derived from the data chunk's size and the block
// alignment, never from a declared sample count. A data chunk that claims
// more bytes than the stream holds — an interrupted recording, a partial
// copy — is clamped to the bytes that exist and File.IsTruncated reports it,
// because refusing to open such a file would make the common repair case
// (look at it, cut it) impossible.
//
// # Reading
//
// Opening reads the header and nothing else. ReadFramesAtE reads exactly the
// bytes one request needs through io.ReaderAt, so the resident cost of an
// 8 GB file is the window the caller asked for. Sequential and random access
// are equally cheap.
package wavfile
