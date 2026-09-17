package play

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixtureRowByName(t *testing.T, name string) cardgridFixtureRow {
	t.Helper()
	rows, err := cardgridFixtureRows()
	require.NoError(t, err)
	for _, r := range rows {
		if r.name == name {
			return r
		}
	}
	t.Fatalf("no fixture row %q", name)
	return cardgridFixtureRow{}
}

// ADR-0245 §SD6: a recording is reduced, not retained.
func TestBuildAudioArtifact(t *testing.T) {
	sweep := fixtureRowByName(t, "Sine sweep")
	a := buildAudioArtifact(string(sweep.content))
	require.Empty(t, a.reason)
	assert.Equal(t, uint32(44100), a.info.SampleRate)
	assert.Equal(t, 3*time.Second, a.info.Duration)
	assert.Equal(t, audioOverviewColumns, a.overview.Columns())
	assert.Greater(t, int(a.overview.Peak), 90, "a sweep at 0.8 of full scale")

	stereo := buildAudioArtifact(string(fixtureRowByName(t, "Gated tone, stereo").content))
	require.Empty(t, stereo.reason)
	assert.Len(t, stereo.overview.Min, 2)

	silence := buildAudioArtifact(string(fixtureRowByName(t, "Silence").content))
	require.Empty(t, silence.reason)
	assert.Zero(t, silence.overview.Peak, "a flat line is a waveform too")

	cut := buildAudioArtifact(string(fixtureRowByName(t, "Truncated recording").content))
	require.Empty(t, cut.reason, "what is there is drawn")
	assert.True(t, cut.info.Truncated)
	assert.Less(t, cut.info.Duration, time.Second+100*time.Millisecond)

	assert.Contains(t, buildAudioArtifact("not a recording").reason, "not a readable WAVE file")
	assert.Contains(t, buildAudioArtifact(string(fixtureRowByName(t, "Harbour at dusk").content)).reason, "not a readable WAVE file")
}

// The now-playing session against the device-less clock: one session, toggled,
// sought, replaced by the next, ended with its result.
func TestAudioSession(t *testing.T) {
	app := &PlayApp{audioNoDevice: true}
	app.audioFollowResult(ResultID(1))
	sweep := string(fixtureRowByName(t, "Sine sweep").content)
	raws := 0
	raw := func() string { raws++; return sweep }
	opened := func(key string) audioState {
		t.Helper()
		var st audioState
		require.Eventually(t, func() bool {
			st = app.audioStateOf(key)
			return st.active && !st.opening
		}, 5*time.Second, 5*time.Millisecond)
		return st
	}

	assert.False(t, app.audioStateOf("a").active)
	app.audioToggle("a", raw)
	st := opened("a")
	assert.True(t, st.playing)
	assert.Contains(t, st.deviceErr, "no output device", "the reason is on screen, and the playhead still moves")
	assert.Equal(t, 1, raws)

	app.audioToggle("a", raw)
	assert.False(t, app.audioStateOf("a").playing, "the second press pauses")
	assert.Equal(t, 1, raws, "the bytes are read once, when the session starts")

	app.audioSeek("a", 0.5)
	assert.InDelta(t, 0.5, app.audioStateOf("a").fraction, 0.01)
	app.audioSeek("b", 0.9)
	assert.InDelta(t, 0.5, app.audioStateOf("a").fraction, 0.01, "a seek on another cell is not this session's")

	app.audioToggle("b", raw)
	opened("b")
	assert.False(t, app.audioStateOf("a").active, "starting a second recording stops the first")

	app.audioFollowResult(ResultID(1))
	assert.True(t, app.audioStateOf("b").active, "the same result keeps the session")
	app.audioFollowResult(ResultID(2))
	assert.Nil(t, app.audio, "a replaced result ends it")
	app.audioStop() // idempotent
}

// Stopping a session that is still opening leaves nothing open behind it.
func TestAudioSessionStoppedWhileOpening(t *testing.T) {
	app := &PlayApp{audioNoDevice: true}
	sweep := string(fixtureRowByName(t, "Sine sweep").content)
	app.audioToggle("a", func() string { return sweep })
	s := app.audio
	app.audioStop()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.opening
	}, 5*time.Second, 5*time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Nil(t, s.sink)
	assert.Nil(t, s.file)
}
