package pcm

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MemSource serves interleaved samples held in memory. It is the source for
// short fixtures and the demo's clips; anything long goes through a decoder.
type MemSource struct {
	format  Format
	samples []float32
}

var _ SourceI = (*MemSource)(nil)

// NewMemSource wraps interleaved samples; len(samples) must be a whole
// number of frames. The slice is retained, not copied.
func NewMemSource(format Format, samples []float32) (src *MemSource, err error) {
	err = format.Validate()
	if err != nil {
		return nil, err
	}
	if len(samples)%int(format.Channels) != 0 {
		return nil, eb.Build().
			Int("samples", len(samples)).
			Uint16("channels", format.Channels).
			Errorf("sample count is not a whole number of frames")
	}
	return &MemSource{format: format, samples: samples}, nil
}

// Format implements [SourceI].
func (inst *MemSource) Format() (format Format) { return inst.format }

// Frames implements [SourceI].
func (inst *MemSource) Frames() (frames int64) {
	return int64(len(inst.samples) / int(inst.format.Channels))
}

// ReadFramesAt implements [SourceI].
func (inst *MemSource) ReadFramesAt(_ context.Context, frameOffset int64, dst []float32) (n int, err error) {
	n, err = ClampRead(inst.format, inst.Frames(), frameOffset, dst)
	if err != nil || n == 0 {
		return n, err
	}
	ch := int(inst.format.Channels)
	start := int(frameOffset) * ch
	copy(dst[:n*ch], inst.samples[start:start+n*ch])
	return n, nil
}

// Close implements [SourceI]; a memory source holds nothing to release.
func (inst *MemSource) Close() (err error) { return nil }
