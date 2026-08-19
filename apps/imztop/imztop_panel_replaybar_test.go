package imztop

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaySpeedLabel(t *testing.T) {
	assert.Equal(t, "0.5×", replaySpeedLabel(0.5))
	assert.Equal(t, "1×", replaySpeedLabel(1))
	assert.Equal(t, "60×", replaySpeedLabel(60))
}

// TestDefaultReplayWindow_OpensOnTheRecentPast pins where replay starts
// looking. It is one value now: the availability strip picks the range, so the
// opening span only decides where the picture is centred.
func TestDefaultReplayWindow_OpensOnTheRecentPast(t *testing.T) {
	w := defaultReplayWindow()
	assert.InDelta(t, defaultReplaySpan.Seconds(), w.To.Sub(w.From).Seconds(), 1)
	assert.WithinDuration(t, time.Now().UTC(), w.To, 5*time.Second)
}

// jogTestSession builds a started session over a synthetic source with a known
// window, for the jog assertions.
func jogTestSession(t *testing.T, from, to time.Time) *ReplaySampler {
	t.Helper()
	session, err := NewReplaySampler(ReplayOptions{
		Source:      newFakeSource(),
		Window:      sysmreplay.Window{From: from, To: to},
		StartPaused: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	session.Start(context.Background())
	return session
}

func TestJogReplay_MovesAWholeWindowEarlier(t *testing.T) {
	inst := newApp()
	to := time.Now().UTC().Add(-time.Hour)
	from := to.Add(-10 * time.Minute)
	session := jogTestSession(t, from, to)

	inst.jogReplay(session, -1)
	got := session.Window()
	assert.WithinDuration(t, from.Add(-10*time.Minute), got.From, time.Second)
	assert.WithinDuration(t, from, got.To, time.Second, "the new window ends where the old one began")
}

func TestJogReplay_MovesAWholeWindowLater(t *testing.T) {
	inst := newApp()
	to := time.Now().UTC().Add(-time.Hour)
	from := to.Add(-10 * time.Minute)
	session := jogTestSession(t, from, to)

	inst.jogReplay(session, +1)
	got := session.Window()
	assert.WithinDuration(t, to, got.From, time.Second, "the new window begins where the old one ended")
	assert.WithinDuration(t, to.Add(10*time.Minute), got.To, time.Second)
}

// TestJogReplay_ClampsAtNow pins that "later" cannot walk into the future,
// which would replay as an empty window and read as a broken transport.
func TestJogReplay_ClampsAtNow(t *testing.T) {
	inst := newApp()
	now := time.Now().UTC()
	to := now.Add(-2 * time.Minute)
	from := to.Add(-10 * time.Minute)
	session := jogTestSession(t, from, to)

	inst.jogReplay(session, +1) // would land 8 minutes in the future
	got := session.Window()

	assert.False(t, got.To.After(time.Now().UTC().Add(time.Second)),
		"the window must not end in the future")
	assert.InDelta(t, 10*time.Minute.Seconds(), got.To.Sub(got.From).Seconds(), 2,
		"clamping must keep the span, not shrink it")
}

// TestJogReplay_ZeroSpanFallsBackToTheChoice covers a session opened with an
// unbounded window, where To - From is not a usable span.
func TestJogReplay_ZeroSpanFallsBackToTheDefault(t *testing.T) {
	inst := newApp()
	session := jogTestSession(t, time.Time{}, time.Time{})

	inst.jogReplay(session, -1)
	got := session.Window()
	assert.False(t, got.From.IsZero(), "a jog must produce a concrete window")
	assert.InDelta(t, defaultReplaySpan.Seconds(), got.To.Sub(got.From).Seconds(), 2)
}
