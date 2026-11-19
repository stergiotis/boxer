package openaichat

import (
	"math"
	"strconv"
	"strings"
)

// Subtitles are rendered from a transcript's segments rather than asked of
// the server (ADR-0383 §SD3): vLLM refuses srt and vtt, and the newer
// OpenAI models answer json only, while both give segments with times.
// A transcript without segments renders as nothing.

// RenderSRT is the transcript as SubRip cues, one per segment, numbered
// from 1; a diarized segment's text is led by its speaker label.
func RenderSRT(t Transcript) (s string) {
	var b strings.Builder
	for i, seg := range t.Segments {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteByte('\n')
		b.WriteString(cueTime(seg.Start, ','))
		b.WriteString(" --> ")
		b.WriteString(cueTime(seg.End, ','))
		b.WriteByte('\n')
		if seg.Speaker != "" {
			b.WriteString(seg.Speaker)
			b.WriteString(": ")
		}
		b.WriteString(cueText(seg.Text))
		b.WriteString("\n\n")
	}
	return b.String()
}

// RenderWebVTT is the transcript as a WebVTT file; a diarized segment
// carries its speaker as a voice span.
func RenderWebVTT(t Transcript) (s string) {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, seg := range t.Segments {
		b.WriteString(cueTime(seg.Start, '.'))
		b.WriteString(" --> ")
		b.WriteString(cueTime(seg.End, '.'))
		b.WriteByte('\n')
		if seg.Speaker != "" {
			b.WriteString("<v ")
			b.WriteString(vttEscape(seg.Speaker))
			b.WriteString(">")
		}
		b.WriteString(vttEscape(cueText(seg.Text)))
		b.WriteString("\n\n")
	}
	return b.String()
}

// cueTime is seconds as hh:mm:ss<sep>mmm, negative and non-finite times
// clamped to zero.
func cueTime(sec float64, sep byte) string {
	if math.IsNaN(sec) || sec < 0 {
		sec = 0
	}
	if math.IsInf(sec, 1) {
		sec = 0
	}
	ms := int64(math.Round(sec * 1000))
	h := ms / 3_600_000
	ms -= h * 3_600_000
	m := ms / 60_000
	ms -= m * 60_000
	se := ms / 1000
	ms -= se * 1000
	var b []byte
	b = appendPadded(b, h, 2)
	b = append(b, ':')
	b = appendPadded(b, m, 2)
	b = append(b, ':')
	b = appendPadded(b, se, 2)
	b = append(b, sep)
	b = appendPadded(b, ms, 3)
	return string(b)
}

func appendPadded(b []byte, v int64, width int) []byte {
	s := strconv.FormatInt(v, 10)
	for i := len(s); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

// cueText is a segment's text with blank lines removed: a blank line ends
// a cue in both formats.
func cueText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	for strings.Contains(s, "\n\n") {
		s = strings.ReplaceAll(s, "\n\n", "\n")
	}
	return s
}

func vttEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
