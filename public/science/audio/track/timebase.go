package track

import (
	"time"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// TimeBase turns a frame index into the time a reader is shown (ADR-0208
// §SD9). The frame is the position; a duration and an instant are both
// derived from it, never the other way round.
//
// The zero value is relative and carries no sample rate, so every conversion
// answers zero; a [TimeBase] taken from a [Track] always carries the
// recording's format.
type TimeBase struct {
	Format pcm.Format
	// Epoch is the wall-clock instant of frame 0. Zero means the base is
	// relative — a track or a clip, whose readouts are durations from the
	// start. Non-zero means it is absolute, and the same frames read as
	// instants.
	Epoch time.Time
}

// IsAbsolute reports whether an epoch is set, and therefore whether
// [TimeBase.FrameToTime] and [TimeBase.TimeToFrame] answer.
func (inst TimeBase) IsAbsolute() (yes bool) {
	return !inst.Epoch.IsZero()
}

// FrameToDuration returns the offset of frame from frame 0. Conversions go
// through [pcm.Format], which does the arithmetic in integers so a
// twelve-hour offset neither overflows nor drifts.
func (inst TimeBase) FrameToDuration(frame int64) (d time.Duration) {
	return inst.Format.FramesToDuration(frame)
}

// DurationToFrame returns the frame at the offset d from frame 0,
// truncating toward zero. It round-trips with [TimeBase.FrameToDuration] to
// within one frame — the frame is the finer unit, so a duration cannot name
// a position between two frames.
func (inst TimeBase) DurationToFrame(d time.Duration) (frame int64) {
	return inst.Format.DurationToFrames(d)
}

// FrameToTime returns the wall-clock instant of frame; ok is false on a
// relative base, where no instant exists.
func (inst TimeBase) FrameToTime(frame int64) (t time.Time, ok bool) {
	if !inst.IsAbsolute() {
		return time.Time{}, false
	}
	return inst.Epoch.Add(inst.FrameToDuration(frame)), true
}

// TimeToFrame returns the frame at the instant t; ok is false on a relative
// base. The frame may be negative or past the end of the recording — an
// annotation given as an instant need not fall inside the file — so a caller
// that indexes with it clamps first.
func (inst TimeBase) TimeToFrame(t time.Time) (frame int64, ok bool) {
	if !inst.IsAbsolute() {
		return 0, false
	}
	return inst.DurationToFrame(t.Sub(inst.Epoch)), true
}
