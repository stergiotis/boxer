package nowplaying_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/audioout/nowplaying"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/wavfile"
)

func tone(t *testing.T) []byte {
	t.Helper()
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	src, err := pcm.NewSynthSource(format, 8000*2, func(frame int64, _ int) float32 {
		return float32(0.5 * math.Sin(2*math.Pi*220*float64(frame)/8000))
	})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, wavfile.Write(context.Background(), &buf, format, wavfile.EncodingPCMInt, 16, src))
	return buf.Bytes()
}

func opened(t *testing.T, p *nowplaying.Player, key string) nowplaying.State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := p.State(key)
		if st.Active && !st.Opening {
			return st
		}
		require.True(t, time.Now().Before(deadline), "the recording did not open")
		time.Sleep(time.Millisecond)
	}
}

// A window with no bus plays on a silent clock: the recording opens off
// the caller's goroutine, plays, pauses and seeks, and the state says why
// nothing is heard. Starting another recording stops the first.
func TestOneRecordingPlaysAtATime(t *testing.T) {
	data := tone(t)
	loads := 0
	load := func(context.Context) ([]byte, error) { loads++; return data, nil }
	p := nowplaying.New(nil, nil)
	assert.False(t, p.State("a").Active)

	p.Toggle("a", load)
	st := opened(t, p, "a")
	assert.True(t, st.Playing)
	assert.Contains(t, st.Note, "no output device")
	p.Toggle("a", load)
	assert.False(t, p.State("a").Playing, "a second toggle pauses")
	p.Seek("a", 0.5)
	st = p.State("a")
	assert.InDelta(t, 0.5, st.Fraction, 0.01)
	assert.InDelta(t, time.Second.Seconds(), st.Position.Seconds(), 0.05)
	p.Seek("b", 0.9)
	assert.InDelta(t, 0.5, p.State("a").Fraction, 0.01, "a seek of another recording moves nothing")
	p.Toggle("a", load)
	assert.True(t, p.State("a").Playing)
	assert.Equal(t, 1, loads, "the bytes are read once")

	p.Toggle("b", load)
	assert.False(t, p.State("a").Active, "starting another stops the first")
	assert.True(t, opened(t, p, "b").Playing)
	p.Stop()
	assert.False(t, p.State("b").Active)
}

// A recording that cannot be read is the player's, not playing, and says
// why; a window that is leaving starts nothing.
func TestARecordingThatCannotBePlayedSaysWhy(t *testing.T) {
	p := nowplaying.New(nil, nil)
	p.Toggle("gone", func(context.Context) ([]byte, error) { return nil, errors.New("file does not exist") })
	st := opened(t, p, "gone")
	assert.False(t, st.Playing)
	assert.Contains(t, st.Note, "file does not exist")
	p.Toggle("noise", func(context.Context) ([]byte, error) { return []byte("not a wave file"), nil })
	st = opened(t, p, "noise")
	assert.False(t, st.Playing)
	assert.Contains(t, st.Note, "not played")

	leaving := nowplaying.New(nil, func(func(context.Context)) bool { return false })
	leaving.Toggle("a", func(context.Context) ([]byte, error) { return tone(t), nil })
	assert.False(t, leaving.State("a").Active)
}
