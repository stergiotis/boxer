package pcm

import (
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Format describes a stream of interleaved float32 frames.
type Format struct {
	// SampleRate is the number of frames per second.
	SampleRate uint32
	// Channels is the number of samples per frame.
	Channels uint16
}

// IsValid reports whether the format can describe a stream at all.
func (inst Format) IsValid() (valid bool) {
	return inst.SampleRate > 0 && inst.Channels > 0
}

// ValidateE returns an error carrying the offending fields when the format
// is not valid.
func (inst Format) ValidateE() (err error) {
	if inst.IsValid() {
		return nil
	}
	return eb.Build().
		Uint32("sampleRate", inst.SampleRate).
		Uint16("channels", inst.Channels).
		Errorf("invalid pcm format")
}

// FramesToDuration converts a frame count to a duration without overflowing
// on long recordings: twelve hours at 48 kHz is 2.07e9 frames, and a naive
// frames*1e9 product leaves int64 far earlier than that.
func (inst Format) FramesToDuration(frames int64) (d time.Duration) {
	if inst.SampleRate == 0 {
		return 0
	}
	rate := int64(inst.SampleRate)
	sec := frames / rate
	rem := frames % rate
	return time.Duration(sec)*time.Second + time.Duration(rem*int64(time.Second)/rate)
}

// DurationToFrames converts a duration to a frame count, truncating toward
// zero.
func (inst Format) DurationToFrames(d time.Duration) (frames int64) {
	rate := int64(inst.SampleRate)
	sec := int64(d / time.Second)
	rem := int64(d % time.Second)
	return sec*rate + rem*rate/int64(time.Second)
}

// SamplesToFrames converts an interleaved sample count to whole frames.
func (inst Format) SamplesToFrames(samples int) (frames int) {
	if inst.Channels == 0 {
		return 0
	}
	return samples / int(inst.Channels)
}
