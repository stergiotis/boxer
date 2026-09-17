package gloss

import (
	"fmt"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

// MediaTypeWAV joins the content family after `application/cbor` (ADR-0245
// §SD6): a column holding a RIFF/WAVE recording. The host's block face is the
// recording's waveform with play and pause; the inline face is what a grid
// wants to know about a recording — how long, at what rate, how many
// channels.
//
// IANA registers no `audio/wav`; what is registered is `audio/vnd.wave`
// (RFC 2361), and what is written in the wild is `audio/wav`, `audio/x-wav`
// and `audio/wave`. A table's `mime` column holds whichever its ingester
// wrote, so all four are members and mean the same thing —
// [IsWAVMediaType] is the test a host keys its block face on.
const (
	MediaTypeWAV = "audio/wav"
	// MediaTypeWAVX is the pre-registration `x-` spelling, still the most
	// common one in stored data.
	MediaTypeWAVX = "audio/x-wav"
	// MediaTypeWAVWave is the spelling some browsers report.
	MediaTypeWAVWave = "audio/wave"
	// MediaTypeWAVVndWave is the registered type (RFC 2361).
	MediaTypeWAVVndWave = "audio/vnd.wave"
)

// IsWAVMediaType reports whether a canonical media type is one of the WAVE
// spellings.
func IsWAVMediaType(mediaType string) bool {
	switch mediaType {
	case MediaTypeWAV, MediaTypeWAVX, MediaTypeWAVWave, MediaTypeWAVVndWave:
		return true
	}
	return false
}

// wavInlineHeaderBytes bounds what the inline face reads of a cell. An inline
// face has no cache and runs per visible cell per frame; a WAVE header is a
// few dozen bytes at the head of the file, so the face reads a prefix and
// nothing else. A file whose `fmt ` and `data` chunks are not within it — one
// that leads with a long LIST chunk — shows the image family's descriptor,
// type and size, and the block face still reads it in full.
const wavInlineHeaderBytes = 4 << 10

// WavInfo is what a WAVE header says about a recording.
type WavInfo struct {
	SampleRate uint32
	Channels   uint16
	Bits       uint16
	Frames     int64
	Duration   time.Duration
	// Truncated reports a header that promises more than the bytes hold.
	Truncated bool
}

// ReadWavInfo reads a recording's header from the head of raw. It reads at
// most the first few kilobytes, whatever raw's length, and no samples.
func ReadWavInfo(raw string) (info WavInfo, err error) {
	head := raw
	if len(head) > wavInlineHeaderBytes {
		head = head[:wavInlineHeaderBytes]
	}
	// A strings.Reader is an io.ReaderAt over the string itself: no copy of
	// the cell. The size handed over is the cell's, so the reader sees a data
	// chunk that runs past the prefix as one that is there — it never reads
	// it — and one that runs past the CELL as truncated.
	f, err := wavfile.NewReaderE(strings.NewReader(head), int64(len(raw)))
	if err != nil {
		return info, err
	}
	format := f.Format()
	return WavInfo{
		SampleRate: format.SampleRate,
		Channels:   format.Channels,
		Bits:       f.BitsPerSample(),
		Frames:     f.Frames(),
		Duration:   format.FramesToDuration(f.Frames()),
		Truncated:  f.IsTruncated(),
	}, nil
}

// wavFace is the inline face: `[audio/wav · 0:03 · 44.1 kHz · 2 ch]`. A header
// that does not read shows the descriptor in the error tone — a bad row is
// findable in a grid of good ones — and a truncated file the warning tone.
func wavFace(mediaType string) func(cell CellI) Inline {
	return func(cell CellI) Inline {
		raw := rawOrText(cell)
		if len(raw) == 0 {
			return Inline{}
		}
		info, err := ReadWavInfo(raw)
		if err != nil {
			return Inline{Text: fmt.Sprintf("[%s · %s]", mediaType, humanize.IBytes(uint64(len(raw)))), Tone: ToneError}
		}
		face := Inline{Text: fmt.Sprintf("[%s · %s · %s · %d ch]", mediaType, FormatClock(info.Duration), formatSampleRate(info.SampleRate), info.Channels)}
		if info.Truncated {
			face.Tone = ToneWarning
		}
		return face
	}
}

// FormatClock writes a duration the way a player does: m:ss, or h:mm:ss from
// an hour up. Truncated, not rounded, so a readout never shows a time the
// recording has not reached.
func FormatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, (s/60)%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// formatSampleRate writes 44100 as `44.1 kHz` and 8000 as `8 kHz`.
func formatSampleRate(hz uint32) string {
	if hz < 1000 {
		return fmt.Sprintf("%d Hz", hz)
	}
	if hz%1000 == 0 {
		return fmt.Sprintf("%d kHz", hz/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(hz)/1000)
}

// wavFamily is the four spellings, each a content gloss of its own so a
// declaration reads back as it was written.
func wavFamily() []GlossI {
	encoding := []ParamSpec{{Name: ParamEncoding, Doc: "reserved for a base64 source (ADR-0123 §SD7); not supported yet"}}
	out := make([]GlossI, 0, 4)
	for _, mt := range []string{MediaTypeWAV, MediaTypeWAVX, MediaTypeWAVWave, MediaTypeWAVVndWave} {
		out = append(out, &simpleGloss{
			mediaType: mt,
			doc:       "the recording's waveform, with play and pause (block); length, rate and channels from the header (inline)",
			params:    encoding, accepts: textLike, inline: wavFace(mt), refuseParam: ParamEncoding,
		})
	}
	return out
}
