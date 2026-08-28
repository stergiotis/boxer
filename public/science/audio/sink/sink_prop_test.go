package sink_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
)

// TestNullTransportInvariants drives a Null through a random command
// sequence and checks the four things every consumer relies on: the position
// stays inside the source, Ended and State agree, Ended pins the position to
// the end, and the position only ever moves backwards through a seek or a
// restarting play.
func TestNullTransportInvariants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		format := pcm.Format{
			SampleRate: rapid.Uint32Range(8000, 192000).Draw(rt, "sampleRate"),
			Channels:   rapid.Uint16Range(1, 2).Draw(rt, "channels"),
		}
		// Up to an hour at the drawn rate, plus the empty source.
		frames := rapid.Int64Range(0, 3600*int64(format.SampleRate)).Draw(rt, "frames")
		src, err := pcm.NewSynthSourceE(format, frames, nil)
		require.NoError(rt, err)
		clock := sink.NewManualClock(time.Unix(0, 0))
		s := sink.NewNull(src, clock)

		commands := []string{"play", "pause", "advance", "seek", "rate"}
		prev := s.Position()
		mayDrop := false
		for range rapid.IntRange(1, 48).Draw(rt, "steps") {
			switch rapid.SampledFrom(commands).Draw(rt, "cmd") {
			case "play":
				// Playing from a position at the end restarts at frame 0.
				mayDrop = mayDrop || prev >= frames
				s.Play()
			case "pause":
				s.Pause()
			case "advance":
				d := rapid.Int64Range(0, int64(2*time.Hour)).Draw(rt, "d")
				clock.Advance(time.Duration(d))
			case "seek":
				frame := rapid.Int64Range(-frames-1024, 2*frames+1024).Draw(rt, "frame")
				require.NoError(rt, s.SeekE(frame))
				mayDrop = true
			case "rate":
				rate := rapid.Float64Range(sink.RateMinExcl+0.01, sink.RateMaxIncl).Draw(rt, "rate")
				require.NoError(rt, s.SetRateE(rate))
			}

			// One observation, in this order: Position settles the transport,
			// so State and Ended describe the same instant.
			pos := s.Position()
			state := s.State()
			ended := s.Ended()

			require.GreaterOrEqual(rt, pos, int64(0))
			require.LessOrEqual(rt, pos, frames)
			if ended {
				require.Equal(rt, sink.StateStopped, state)
				require.Equal(rt, frames, pos)
			}
			if state == sink.StatePlaying {
				require.Less(rt, pos, frames)
			}
			if !mayDrop {
				require.GreaterOrEqual(rt, pos, prev)
			}
			prev = pos
			mayDrop = false
		}
		require.NoError(rt, s.CloseE())
	})
}

// TestNullProjectionIsIndependentOfPolling checks the property that rules out
// an incremental tick: how often the position is read must not change what it
// reads. A drift-accumulating implementation fails this at the frame rate a
// widget polls at.
func TestNullProjectionIsIndependentOfPolling(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		format := pcm.Format{
			SampleRate: rapid.Uint32Range(8000, 192000).Draw(rt, "sampleRate"),
			Channels:   1,
		}
		frames := 12 * 3600 * int64(format.SampleRate)
		rate := rapid.Float64Range(sink.RateMinExcl+0.01, sink.RateMaxIncl).Draw(rt, "rate")
		polls := rapid.IntRange(1, 200).Draw(rt, "polls")
		step := time.Duration(rapid.Int64Range(1, int64(time.Second)).Draw(rt, "step"))

		build := func(poll bool) (pos int64) {
			src, err := pcm.NewSynthSourceE(format, frames, nil)
			require.NoError(rt, err)
			clock := sink.NewManualClock(time.Unix(0, 0))
			s := sink.NewNull(src, clock)
			require.NoError(rt, s.SetRateE(rate))
			s.Play()
			for range polls {
				clock.Advance(step)
				if poll {
					_ = s.Position()
				}
			}
			return s.Position()
		}
		require.Equal(rt, build(false), build(true))
	})
}
