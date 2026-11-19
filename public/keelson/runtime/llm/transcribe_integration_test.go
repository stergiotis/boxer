//go:build integration

package llm

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// toneWav is one second of a quiet 440 Hz tone, 16 kHz mono 16-bit PCM:
// enough for a server to accept and decode, with nothing to say.
func toneWav() []byte {
	const rate, n = 16000, 16000
	b := make([]byte, 44+2*n)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+2*n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], rate)
	binary.LittleEndian.PutUint32(b[28:], rate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 2*n)
	for i := 0; i < n; i++ {
		v := int16(800 * math.Sin(2*math.Pi*440*float64(i)/rate))
		binary.LittleEndian.PutUint16(b[44+2*i:], uint16(v))
	}
	return b
}

// TestTranscribeAgainstALiveRoute sends a recording through the service to
// the route BOXER_LLM_TRANSCRIBE_* configures — an OpenAI account or a
// vLLM — plain, with segment times and streamed, and diarized when a
// diarizing model is configured. Skipped when no route is.
func TestTranscribeAgainstALiveRoute(t *testing.T) {
	tc := TranscribeConfigFromEnv()
	if !tc.Configured() {
		t.Skip("BOXER_LLM_TRANSCRIBE_ENDPOINT and BOXER_LLM_TRANSCRIBE_MODEL are unset")
	}
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := NewService(bus, zerolog.Nop(), Config{Transcribe: tc, Journal: AcceptAnyJournal{}})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli := NewClient(bus.NewClient(appId, ClientCaps("test: transcribe live")))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	audio := toneWav()

	res, err := cli.Transcribe(ctx, TranscriptionRequest{Audio: audio, MediaType: "audio/wav", Language: "en"})
	require.NoError(t, err)
	assert.NotEqual(t, openaichat.TranscriptFormatText, res.Transcript.Format, "the route answered JSON")
	t.Logf("plain: %q format=%s usage=%+v", res.Transcript.Text, res.Transcript.Format, res.Transcript.Usage)

	res, err = cli.Transcribe(ctx, TranscriptionRequest{Audio: audio, MediaType: "audio/wav", Language: "en", Timestamps: openaichat.TimestampsSegment})
	if err != nil {
		// gpt-4o-transcribe and its siblings, and vLLM models without
		// segment times, refuse verbose_json; the refusal must be the
		// provider's 400, classified.
		require.ErrorIs(t, err, openaichat.ErrBadRequest)
		t.Logf("segment times refused by this model: %v", err)
	} else {
		t.Logf("segments: %d duration=%v", len(res.Transcript.Segments), res.Transcript.Duration)
	}

	// The number of deltas is not asserted: whisper-1 ignores stream and
	// answers whole, and a silent tone may carry no text to stream. What
	// must hold is that a streamed request is answered, completely.
	var deltas int
	res, err = cli.TranscribeStreaming(ctx, TranscriptionRequest{Audio: audio, MediaType: "audio/wav", Language: "en"}, func(d Delta) { deltas++ })
	require.NoError(t, err, "a stream that ended early is an error, not a transcript")
	t.Logf("streamed: %q in %d deltas", res.Transcript.Text, deltas)

	if tc.DiarizeModel != "" {
		res, err = cli.Transcribe(ctx, TranscriptionRequest{Audio: audio, MediaType: "audio/wav", Diarize: true})
		require.NoError(t, err)
		t.Logf("diarized: %d segments, %d speakers, format=%s", len(res.Transcript.Segments), res.Transcript.Speakers(), res.Transcript.Format)
	}
	for _, c := range svc.Calls() {
		assert.Equal(t, VerbTranscribe, c.Verb)
	}
}
