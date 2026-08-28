package track

import (
	"context"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// lockedSource makes one [pcm.SourceI] usable from several goroutines by
// serialising the calls the contract restricts to one at a time. It is the
// seam ADR-0208 §SD1 needs so that one open recording can serve both the
// frame thread and the sink's callback goroutine (§SD6): the peaks build,
// [Track.ReadWindowE] and whatever sink [Options.NewSink] returns all hold
// this adapter rather than the source it wraps.
//
// Format and Frames are immutable for the source's lifetime, so they are
// copied at construction and answered without the lock — the frame thread
// asks for them per rendered frame and must not queue behind a decoder's
// read.
type lockedSource struct {
	mu     sync.Mutex
	src    pcm.SourceI
	closed bool

	format pcm.Format
	frames int64
}

var _ pcm.SourceI = (*lockedSource)(nil)

func newLockedSource(src pcm.SourceI) (inst *lockedSource) {
	frames := src.Frames()
	if frames < 0 {
		frames = 0
	}
	return &lockedSource{
		src:    src,
		format: src.Format(),
		frames: frames,
	}
}

// Format implements [pcm.SourceI].
func (inst *lockedSource) Format() (format pcm.Format) { return inst.format }

// Frames implements [pcm.SourceI].
func (inst *lockedSource) Frames() (frames int64) { return inst.frames }

// ReadFramesAtE implements [pcm.SourceI], delegating the whole read contract
// — the partial read at the end, io.EOF past it, the error on a negative
// offset — to the wrapped source. Concurrent calls are serialised, so a
// caller waits for the read in flight; on a closed adapter it is an error
// rather than the undefined behaviour the source contract allows.
func (inst *lockedSource) ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return 0, eb.Build().Int64("frameOffset", frameOffset).Errorf("read from a closed source")
	}
	return inst.src.ReadFramesAtE(ctx, frameOffset, dst)
}

// CloseE implements [pcm.SourceI]. It is idempotent, and it waits for a read
// in flight rather than closing underneath it.
func (inst *lockedSource) CloseE() (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil
	}
	inst.closed = true
	return inst.src.CloseE()
}
