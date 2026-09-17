package gloss

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testWAV(t *testing.T, format pcm.Format, seconds float64) []byte {
	t.Helper()
	frames := int64(seconds * float64(format.SampleRate))
	src, err := pcm.NewSynthSourceE(format, frames, pcm.Sine(format, 440, 0.5))
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, wavfile.WriteE(context.Background(), &buf, format, wavfile.EncodingPCMInt, 16, src))
	return buf.Bytes()
}

// ADR-0245 §Verification: the inline face over good, truncated and
// header-far files.
func TestWavInlineFace(t *testing.T) {
	cat := Default()
	for _, mt := range []string{MediaTypeWAV, MediaTypeWAVX, MediaTypeWAVWave, MediaTypeWAVVndWave} {
		_, ok := cat.Lookup(mt)
		assert.True(t, ok, mt)
		assert.True(t, IsWAVMediaType(mt))
	}
	assert.False(t, IsWAVMediaType("audio/flac"))

	_, _, inst, err := cat.BindToken(MediaTypeWAV)
	require.NoError(t, err)
	ok, _ := inst.Accepts(ValueKindBytes)
	assert.True(t, ok)
	ok, reason := inst.Accepts(ValueKindNumeric)
	assert.False(t, ok)
	assert.NotEmpty(t, reason)
	_, _, _, err = cat.BindToken(MediaTypeWAV + ";encoding=base64")
	assert.Error(t, err, "the column must hold the bytes")

	good := testWAV(t, pcm.Format{SampleRate: 44100, Channels: 2}, 3.5)
	face := inst.Inline(TextCell{S: string(good), K: ValueKindBytes})
	assert.Equal(t, "[audio/wav · 0:03 · 44.1 kHz · 2 ch]", face.Text)
	assert.Equal(t, ToneNeutral, face.Tone)

	face = inst.Inline(TextCell{S: string(good[:len(good)/3]), K: ValueKindBytes})
	assert.Equal(t, "[audio/wav · 0:01 · 44.1 kHz · 2 ch]", face.Text, "the length is what the bytes hold, not what the header promised")
	assert.Equal(t, ToneWarning, face.Tone, "and the tone says it is not all there")

	face = inst.Inline(TextCell{S: "definitely not a recording", K: ValueKindBytes})
	assert.Equal(t, "[audio/wav · 26 B]", face.Text)
	assert.Equal(t, ToneError, face.Tone)

	assert.Equal(t, Inline{}, inst.Inline(TextCell{S: "", K: ValueKindBytes}))
}

// A header past the prefix the inline face reads is a descriptor, not a
// parse of the whole cell: the face runs per visible cell per frame.
func TestWavInlineFaceReadsOnlyThePrefix(t *testing.T) {
	good := testWAV(t, pcm.Format{SampleRate: 8000, Channels: 1}, 1)
	// RIFF header (12 bytes), then a LIST chunk long enough to push `fmt `
	// past the prefix, then the original chunks.
	pad := make([]byte, wavInlineHeaderBytes)
	far := append([]byte{}, good[:12]...)
	far = append(far, 'L', 'I', 'S', 'T', byte(len(pad)), byte(len(pad)>>8), byte(len(pad)>>16), byte(len(pad)>>24))
	far = append(far, pad...)
	far = append(far, good[12:]...)
	_, err := ReadWavInfo(string(far))
	assert.Error(t, err)

	info, err := ReadWavInfo(string(good))
	require.NoError(t, err)
	assert.Equal(t, WavInfo{SampleRate: 8000, Channels: 1, Bits: 16, Frames: 8000, Duration: time.Second}, info)

	big := testWAV(t, pcm.Format{SampleRate: 48000, Channels: 2}, 20) // ~3.7 MiB
	s := string(big)
	allocs := testing.AllocsPerRun(20, func() { _, _ = ReadWavInfo(s) })
	assert.Less(t, allocs, float64(12), "a header read, whatever the recording's size")
}

func TestFormatClock(t *testing.T) {
	assert.Equal(t, "0:00", FormatClock(0))
	assert.Equal(t, "0:03", FormatClock(3999*time.Millisecond))
	assert.Equal(t, "1:05", FormatClock(65*time.Second))
	assert.Equal(t, "12:00:01", FormatClock(12*time.Hour+time.Second))
	assert.Equal(t, "0:00", FormatClock(-time.Second))
}
