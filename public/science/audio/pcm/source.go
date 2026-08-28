package pcm

import (
	"context"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SourceI is a positioned, seekable reader of interleaved float32 frames.
//
// Implementations are safe for use from one goroutine at a time; callers
// that read from several goroutines serialise the calls themselves.
type SourceI interface {
	// Format returns the stream's format; it does not change over the
	// source's lifetime.
	Format() Format
	// Frames returns the total number of frames. It is known up front for
	// every source in scope (a WAV header, an ffprobe run); streams of
	// unknown length are not part of this contract.
	Frames() int64
	// ReadFramesAtE fills dst with interleaved frames starting at
	// frameOffset and returns the number of frames read.
	//
	//   - dst is consumed in whole frames: len(dst)/Channels frames at most,
	//     any remainder of dst untouched.
	//   - A read that starts before Frames() returns n > 0 and err == nil,
	//     with n < requested only when the source ends inside dst.
	//   - A read that starts at or past Frames() returns 0, io.EOF.
	//   - A negative frameOffset is an error other than io.EOF.
	//
	// Reads may be expensive when frameOffset moves backwards; see the
	// package documentation.
	ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error)
	// CloseE releases whatever the source holds. Reading after CloseE is
	// undefined.
	CloseE() (err error)
}

// ClampReadE applies the shared bounds arithmetic of the read contract: it
// validates frameOffset against frames and returns how many whole frames of
// dst may be filled. Decoders call it first so every implementation agrees
// on the edge cases; it returns (0, io.EOF) at or past the end.
func ClampReadE(format Format, frames int64, frameOffset int64, dst []float32) (n int, err error) {
	if frameOffset < 0 {
		return 0, eb.Build().Int64("frameOffset", frameOffset).Errorf("negative frame offset")
	}
	if frameOffset >= frames {
		return 0, io.EOF
	}
	n = format.SamplesToFrames(len(dst))
	if remaining := frames - frameOffset; int64(n) > remaining {
		n = int(remaining)
	}
	return n, nil
}
