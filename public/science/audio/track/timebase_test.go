package track_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/track"
)

func absInt64(v int64) (a int64) {
	if v < 0 {
		return -v
	}
	return v
}

func drawFormat(rt *rapid.T) (format pcm.Format) {
	return pcm.Format{
		SampleRate: rapid.Uint32Range(8000, 192000).Draw(rt, "sampleRate"),
		Channels:   rapid.Uint16Range(1, 8).Draw(rt, "channels"),
	}
}

// TestTimeBaseFrameRoundTrip is ADR-0208 §SD9's claim that the frame is the
// position: a frame converted to a duration and back names the same frame,
// give or take the one frame a duration cannot resolve. It holds before frame
// 0 as well, because an annotation given as an instant may precede the
// recording.
func TestTimeBaseFrameRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		format := drawFormat(rt)
		span := 12 * 3600 * int64(format.SampleRate)
		frame := rapid.Int64Range(-span, span).Draw(rt, "frame")
		tb := track.TimeBase{Format: format}

		d := tb.FrameToDuration(frame)
		back := tb.DurationToFrame(d)
		require.LessOrEqual(rt, absInt64(frame-back), int64(1), "frame %d became %v became frame %d", frame, d, back)
		if frame == 0 {
			require.Equal(rt, time.Duration(0), d)
		}
	})
}

// TestTimeBaseDurationRoundTrip is the other direction: a duration lands on
// the frame it falls in, whose own duration is within one frame of it.
func TestTimeBaseDurationRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		format := drawFormat(rt)
		tb := track.TimeBase{Format: format}
		d := time.Duration(rapid.Int64Range(-int64(12*time.Hour), int64(12*time.Hour)).Draw(rt, "d"))

		frame := tb.DurationToFrame(d)
		back := tb.FrameToDuration(frame)
		// One frame is floor(1e9/rate) ns; the two truncations can each lose
		// a nanosecond on top of it.
		bound := tb.FrameToDuration(1) + 2
		require.LessOrEqual(rt, absInt64(int64(d-back)), int64(bound), "%v became frame %d became %v", d, frame, back)
	})
}

// TestTimeBaseRelativeHasNoInstants is the half of §SD9 a caller has to
// branch on: with no epoch there is no wall-clock reading at all, and the
// duration reading still answers.
func TestTimeBaseRelativeHasNoInstants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		format := drawFormat(rt)
		tb := track.TimeBase{Format: format}
		require.False(rt, tb.IsAbsolute())

		frame := rapid.Int64Range(0, 12*3600*int64(format.SampleRate)).Draw(rt, "frame")
		at, ok := tb.FrameToTime(frame)
		require.False(rt, ok)
		require.True(rt, at.IsZero())

		back, ok := tb.TimeToFrame(time.Date(2026, time.August, 28, 9, 30, 0, 0, time.UTC))
		require.False(rt, ok)
		require.Equal(rt, int64(0), back)
	})
}

// TestTimeBaseAbsoluteRoundTrip is the other half: with an epoch the same
// frames read as instants, and the two readings of a position agree.
func TestTimeBaseAbsoluteRoundTrip(t *testing.T) {
	base := time.Date(2026, time.August, 28, 9, 30, 0, 0, time.UTC)
	rapid.Check(t, func(rt *rapid.T) {
		format := drawFormat(rt)
		epoch := base.Add(time.Duration(rapid.Int64Range(-int64(24*time.Hour), int64(24*time.Hour)).Draw(rt, "epochOffset")))
		tb := track.TimeBase{Format: format, Epoch: epoch}
		require.True(rt, tb.IsAbsolute())

		span := 12 * 3600 * int64(format.SampleRate)
		frame := rapid.Int64Range(-span, span).Draw(rt, "frame")
		at, ok := tb.FrameToTime(frame)
		require.True(rt, ok)
		back, ok := tb.TimeToFrame(at)
		require.True(rt, ok)
		require.LessOrEqual(rt, absInt64(frame-back), int64(1), "frame %d became %v became frame %d", frame, at, back)

		// The relative and the absolute reading of one position are the same
		// offset; a user flipping between them sees no jump.
		require.Equal(rt, tb.FrameToDuration(frame), at.Sub(epoch))
		at0, ok := tb.FrameToTime(0)
		require.True(rt, ok)
		require.True(rt, at0.Equal(epoch))
	})
}

// TestTimeBaseZeroValue pins what a zero TimeBase answers, since it is
// reachable by a caller that builds one by hand rather than taking a track's.
func TestTimeBaseZeroValue(t *testing.T) {
	var tb track.TimeBase
	require.False(t, tb.IsAbsolute())
	require.Equal(t, time.Duration(0), tb.FrameToDuration(48000))
	require.Equal(t, int64(0), tb.DurationToFrame(time.Second))
	_, ok := tb.FrameToTime(0)
	require.False(t, ok)
	_, ok = tb.TimeToFrame(time.Now())
	require.False(t, ok)
}
