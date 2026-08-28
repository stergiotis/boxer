package pcm

import (
	"context"
	"math"
)

// SampleFunc computes the sample of channel ch at frame index frame. It must
// be a pure function of its arguments so a [SynthSource] reads the same
// bytes however the reads are chunked.
type SampleFunc func(frame int64, ch int) (sample float32)

// SynthSource is a procedural source: any length, no storage, deterministic.
// It is how a twelve-hour file is exercised in a benchmark without an 8 GB
// fixture, and what the widget demo plays.
type SynthSource struct {
	format Format
	frames int64
	fn     SampleFunc
}

var _ SourceI = (*SynthSource)(nil)

// NewSynthSourceE builds a source of the given length over fn.
func NewSynthSourceE(format Format, frames int64, fn SampleFunc) (src *SynthSource, err error) {
	err = format.ValidateE()
	if err != nil {
		return nil, err
	}
	if frames < 0 {
		frames = 0
	}
	if fn == nil {
		fn = Silence()
	}
	return &SynthSource{format: format, frames: frames, fn: fn}, nil
}

// Format implements [SourceI].
func (inst *SynthSource) Format() (format Format) { return inst.format }

// Frames implements [SourceI].
func (inst *SynthSource) Frames() (frames int64) { return inst.frames }

// ReadFramesAtE implements [SourceI].
func (inst *SynthSource) ReadFramesAtE(_ context.Context, frameOffset int64, dst []float32) (n int, err error) {
	n, err = ClampReadE(inst.format, inst.frames, frameOffset, dst)
	if err != nil || n == 0 {
		return n, err
	}
	ch := int(inst.format.Channels)
	i := 0
	for f := range n {
		frame := frameOffset + int64(f)
		for c := range ch {
			dst[i] = inst.fn(frame, c)
			i++
		}
	}
	return n, nil
}

// CloseE implements [SourceI]; a procedural source holds nothing to release.
func (inst *SynthSource) CloseE() (err error) { return nil }

// Silence is the all-zero signal.
func Silence() (fn SampleFunc) {
	return func(_ int64, _ int) float32 { return 0 }
}

// Sine is a steady tone of freqHz at amplitude amp on every channel.
func Sine(format Format, freqHz float64, amp float32) (fn SampleFunc) {
	step := 2 * math.Pi * freqHz / float64(format.SampleRate)
	return func(frame int64, _ int) float32 {
		return amp * float32(math.Sin(step*float64(frame)))
	}
}

// Chirp sweeps linearly from f0Hz to f1Hz over the source's length in
// frames, on every channel.
func Chirp(format Format, frames int64, f0Hz, f1Hz float64, amp float32) (fn SampleFunc) {
	rate := float64(format.SampleRate)
	total := float64(max(frames, 1))
	return func(frame int64, _ int) float32 {
		t := float64(frame) / rate
		k := (f1Hz - f0Hz) / (total / rate)
		phase := 2 * math.Pi * (f0Hz*t + 0.5*k*t*t)
		return amp * float32(math.Sin(phase))
	}
}

// Gate multiplies inner by a square wave: on for onFrames, off for
// offFrames, repeating. Speech-shaped test signals are a tone under a gate.
func Gate(inner SampleFunc, onFrames, offFrames int64) (fn SampleFunc) {
	period := max(onFrames+offFrames, 1)
	return func(frame int64, ch int) float32 {
		if frame%period < onFrames {
			return inner(frame, ch)
		}
		return 0
	}
}

// PerChannel dispatches to one function per channel; channels past the
// slice are silent.
func PerChannel(fns ...SampleFunc) (fn SampleFunc) {
	return func(frame int64, ch int) float32 {
		if ch < 0 || ch >= len(fns) || fns[ch] == nil {
			return 0
		}
		return fns[ch](frame, ch)
	}
}
