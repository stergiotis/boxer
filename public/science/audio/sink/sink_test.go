package sink_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

const testSampleRate uint32 = 48000

// newTestNull returns a Null over a silent source of the given length in
// seconds, driven by a clock the caller moves.
func newTestNull(t *testing.T, seconds int64) (inst *sink.Null, clock *sink.ManualClock, frames int64) {
	t.Helper()
	format := pcm.Format{SampleRate: testSampleRate, Channels: 2}
	frames = seconds * int64(testSampleRate)
	src, err := pcm.NewSynthSourceE(format, frames, nil)
	require.NoError(t, err)
	clock = sink.NewManualClock(time.Unix(0, 0))
	return sink.NewNull(src, clock), clock, frames
}

func TestNullInitialState(t *testing.T) {
	s, clock, frames := newTestNull(t, 10)
	require.Equal(t, frames, s.Frames())
	require.Equal(t, testSampleRate, s.Format().SampleRate)
	require.Equal(t, sink.StateStopped, s.State())
	require.Equal(t, int64(0), s.Position())
	require.False(t, s.Ended())
	require.InDelta(t, 1.0, s.Rate(), 0)
	require.InDelta(t, 1.0, s.Volume(), 0)

	// A stopped sink does not move with the clock.
	clock.Advance(time.Second)
	require.Equal(t, int64(0), s.Position())
	require.Equal(t, sink.StateStopped, s.State())
}

func TestNullPlayAdvancesWithTheClock(t *testing.T) {
	s, clock, _ := newTestNull(t, 10)
	s.Play()
	require.Equal(t, sink.StatePlaying, s.State())
	require.Equal(t, int64(0), s.Position())

	clock.Advance(time.Second)
	require.Equal(t, int64(testSampleRate), s.Position())
	clock.Advance(500 * time.Millisecond)
	require.Equal(t, int64(testSampleRate)*3/2, s.Position())

	// Play is idempotent: it does not re-anchor a running transport.
	s.Play()
	require.Equal(t, int64(testSampleRate)*3/2, s.Position())
	require.Equal(t, sink.StatePlaying, s.State())
}

func TestNullPauseFreezesAndResumeContinues(t *testing.T) {
	s, clock, _ := newTestNull(t, 10)
	s.Play()
	clock.Advance(time.Second)
	s.Pause()
	require.Equal(t, sink.StatePaused, s.State())
	require.Equal(t, int64(testSampleRate), s.Position())

	clock.Advance(3 * time.Second)
	require.Equal(t, int64(testSampleRate), s.Position(), "a paused sink ignores the clock")
	s.Pause()
	require.Equal(t, int64(testSampleRate), s.Position(), "pause is idempotent")

	s.Play()
	clock.Advance(time.Second)
	require.Equal(t, 2*int64(testSampleRate), s.Position())
}

func TestNullRateScalesAdvance(t *testing.T) {
	s, clock, _ := newTestNull(t, 10)
	require.NoError(t, s.SetRateE(2))
	require.InDelta(t, 2.0, s.Rate(), 0)
	s.Play()
	clock.Advance(time.Second)
	require.Equal(t, 2*int64(testSampleRate), s.Position())

	// A rate change during playback re-anchors: the playhead keeps the
	// position it had and only its slope changes.
	require.NoError(t, s.SetRateE(0.5))
	require.Equal(t, 2*int64(testSampleRate), s.Position())
	clock.Advance(time.Second)
	require.Equal(t, 2*int64(testSampleRate)+int64(testSampleRate)/2, s.Position())
}

func TestNullRateRange(t *testing.T) {
	s, _, _ := newTestNull(t, 10)
	for _, rate := range []float64{0, -1, 0.25, 0.1, 4.0001, 8, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Error(t, s.SetRateE(rate), "rate %v", rate)
		require.InDelta(t, 1.0, s.Rate(), 0, "a rejected rate leaves the rate alone")
	}
	for _, rate := range []float64{0.2501, 1, 4} {
		require.NoError(t, s.SetRateE(rate), "rate %v", rate)
		require.InDelta(t, rate, s.Rate(), 0)
	}
}

func TestNullVolume(t *testing.T) {
	s, _, _ := newTestNull(t, 10)
	require.NoError(t, s.SetVolumeE(0))
	require.InDelta(t, 0.0, s.Volume(), 0)
	require.NoError(t, s.SetVolumeE(0.25))
	require.InDelta(t, 0.25, s.Volume(), 0)
	for _, v := range []float64{-0.001, 1.001, math.NaN(), math.Inf(1)} {
		require.Error(t, s.SetVolumeE(v), "volume %v", v)
		require.InDelta(t, 0.25, s.Volume(), 0)
	}
	// Volume does not touch the transport.
	s.Play()
	require.NoError(t, s.SetVolumeE(1))
	require.Equal(t, sink.StatePlaying, s.State())
}

func TestNullSeekClampsAndKeepsState(t *testing.T) {
	s, clock, frames := newTestNull(t, 10)
	require.NoError(t, s.SeekE(-100))
	require.Equal(t, int64(0), s.Position())
	require.Equal(t, sink.StateStopped, s.State())

	require.NoError(t, s.SeekE(frames+10_000))
	require.Equal(t, frames, s.Position())
	require.False(t, s.Ended(), "a seek to the end is a position, not the end of playback")
	require.Equal(t, sink.StateStopped, s.State())

	// Seeking while paused keeps the sink paused and re-anchors it.
	require.NoError(t, s.SeekE(1234))
	s.Play()
	clock.Advance(time.Second)
	s.Pause()
	require.Equal(t, 1234+int64(testSampleRate), s.Position())
	require.NoError(t, s.SeekE(7))
	require.Equal(t, sink.StatePaused, s.State())
	clock.Advance(time.Second)
	require.Equal(t, int64(7), s.Position())
}

func TestNullSeekToEndWhilePlayingEnds(t *testing.T) {
	s, _, frames := newTestNull(t, 10)
	s.Play()
	require.NoError(t, s.SeekE(frames))
	// The sink is playing at the end of the source, so it runs into it on
	// the next observation even without the clock moving.
	require.Equal(t, frames, s.Position())
	require.True(t, s.Ended())
	require.Equal(t, sink.StateStopped, s.State())
}

func TestNullPlaybackStopsExactlyAtTheEnd(t *testing.T) {
	s, clock, frames := newTestNull(t, 10)
	s.Play()
	clock.Advance(9 * time.Second)
	require.Equal(t, 9*int64(testSampleRate), s.Position())
	require.False(t, s.Ended())
	require.Equal(t, sink.StatePlaying, s.State())

	clock.Advance(5 * time.Second)
	require.Equal(t, frames, s.Position())
	require.True(t, s.Ended())
	require.Equal(t, sink.StateStopped, s.State())

	// It stays there.
	clock.Advance(time.Hour)
	require.Equal(t, frames, s.Position())
	require.True(t, s.Ended())
}

func TestNullPlayAfterEndRestartsFromZero(t *testing.T) {
	s, clock, frames := newTestNull(t, 10)
	s.Play()
	clock.Advance(11 * time.Second)
	require.True(t, s.Ended())

	s.Play()
	require.False(t, s.Ended())
	require.Equal(t, sink.StatePlaying, s.State())
	require.Equal(t, int64(0), s.Position())
	clock.Advance(time.Second)
	require.Equal(t, int64(testSampleRate), s.Position())

	// A seek clears Ended too.
	clock.Advance(time.Hour)
	require.True(t, s.Ended())
	require.NoError(t, s.SeekE(frames/2))
	require.False(t, s.Ended())
	require.Equal(t, frames/2, s.Position())
}

func TestNullPlayFromASeekToTheEndRestarts(t *testing.T) {
	s, _, frames := newTestNull(t, 10)
	require.NoError(t, s.SeekE(frames))
	require.False(t, s.Ended())
	s.Play()
	require.Equal(t, int64(0), s.Position(), "playing from a position at the end restarts")
}

func TestNullClockMovedBackwardsProjectsBack(t *testing.T) {
	s, clock, _ := newTestNull(t, 10)
	require.NoError(t, s.SeekE(1000))
	s.Play()
	clock.Advance(2 * time.Second)
	require.Equal(t, 1000+2*int64(testSampleRate), s.Position())

	// The position is a projection of the clock, so a clock moved backwards
	// moves it back — but never behind the anchor it projects from.
	clock.Advance(-time.Second)
	require.Equal(t, 1000+int64(testSampleRate), s.Position())
	clock.Advance(-time.Hour)
	require.Equal(t, int64(1000), s.Position())
	require.Equal(t, sink.StatePlaying, s.State())
}

func TestNullTwelveHourPositionIsExact(t *testing.T) {
	// The long-file case ADR-0208 is shaped around: the projection must not
	// lose frames to float64 rounding.
	s, clock, frames := newTestNull(t, 12*3600)
	s.Play()
	clock.Advance(12*time.Hour - time.Second)
	require.Equal(t, frames-int64(testSampleRate), s.Position())
	clock.Advance(time.Second)
	require.Equal(t, frames, s.Position())
	require.True(t, s.Ended())
}

func TestNullClose(t *testing.T) {
	s, clock, _ := newTestNull(t, 10)
	s.Play()
	clock.Advance(time.Second)
	require.NoError(t, s.CloseE())

	require.Equal(t, sink.StateStopped, s.State())
	require.Equal(t, int64(testSampleRate), s.Position(), "the position is frozen where it was closed")
	clock.Advance(time.Hour)
	require.Equal(t, int64(testSampleRate), s.Position())

	s.Play()
	require.Equal(t, sink.StateStopped, s.State(), "play is a no-op on a closed sink")
	s.Pause()
	require.Equal(t, sink.StateStopped, s.State())
	require.Error(t, s.SeekE(0))
	require.Error(t, s.SetRateE(2))
	require.Error(t, s.SetVolumeE(0.5))
	require.InDelta(t, 1.0, s.Rate(), 0)
	require.InDelta(t, 1.0, s.Volume(), 0)
	require.NoError(t, s.CloseE(), "close is idempotent")
}

func TestNullClosedWhilePlayingAtTheEndStaysEnded(t *testing.T) {
	s, clock, frames := newTestNull(t, 10)
	s.Play()
	clock.Advance(11 * time.Second)
	require.NoError(t, s.CloseE())
	require.True(t, s.Ended())
	require.Equal(t, frames, s.Position())
}

func TestNullNilSourceAndNilClock(t *testing.T) {
	s := sink.NewNull(nil, nil)
	require.Equal(t, int64(0), s.Frames())
	require.Equal(t, int64(0), s.Position())
	s.Play()
	require.Equal(t, int64(0), s.Position())
	require.True(t, s.Ended(), "an empty source ends as soon as it is played")
	require.Equal(t, sink.StateStopped, s.State())
	require.NoError(t, s.CloseE())
}

func TestNullRealClockAdvances(t *testing.T) {
	format := pcm.Format{SampleRate: testSampleRate, Channels: 1}
	src, err := pcm.NewSynthSourceE(format, 3600*int64(testSampleRate), nil)
	require.NoError(t, err)
	s := sink.NewNull(src, nil)
	s.Play()
	first := s.Position()
	// Busy-wait on the process clock rather than sleeping, so the test does
	// not depend on a scheduler deadline.
	deadline := time.Now().Add(time.Second)
	var second int64
	for time.Now().Before(deadline) {
		second = s.Position()
		if second > first {
			break
		}
	}
	require.Greater(t, second, first)
	require.NoError(t, s.CloseE())
}

func TestManualClock(t *testing.T) {
	var zero sink.ManualClock
	require.True(t, zero.Now().IsZero(), "the zero value is a usable clock")
	zero.Advance(time.Second)
	require.Equal(t, time.Second, zero.Now().Sub(time.Time{}))

	clock := sink.NewManualClock(time.Unix(1000, 0))
	require.Equal(t, time.Unix(1000, 0), clock.Now())
	clock.Advance(-500 * time.Second)
	require.Equal(t, time.Unix(500, 0), clock.Now())
	clock.Set(time.Unix(7, 0))
	require.Equal(t, time.Unix(7, 0), clock.Now())
}

func TestStateEString(t *testing.T) {
	require.Len(t, sink.AllStates, 3)
	seen := make(map[string]struct{}, len(sink.AllStates))
	for _, state := range sink.AllStates {
		seen[state.String()] = struct{}{}
	}
	require.Len(t, seen, len(sink.AllStates))
	require.Equal(t, "unknown", sink.StateE(200).String())
}

func TestNullConcurrentTransportAndPolling(t *testing.T) {
	s, clock, frames := newTestNull(t, 600)
	const steps = 4000

	var badPosition int64 = -1
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range steps {
			switch {
			case i%11 == 0:
				s.Play()
			case i%17 == 0:
				s.Pause()
			}
			clock.Advance(10 * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for range steps {
			pos := s.Position()
			if pos < 0 || pos > frames {
				badPosition = pos
			}
			_ = s.State()
			_ = s.Ended()
			_ = s.Rate()
		}
	}()
	wg.Wait()

	require.Equal(t, int64(-1), badPosition, "position left [0, Frames()]")
	require.NoError(t, s.CloseE())
}
