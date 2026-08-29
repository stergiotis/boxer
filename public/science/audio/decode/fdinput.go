package decode

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// FdInputI is a recording an external decoder must read but that has no
// filesystem path: a decrypted staging object, an anonymous file, anything a
// host holds open rather than names. ffmpeg and ffprobe are separate
// processes, so what they can be given is a descriptor — [childFdPath] is the
// argument that names it inside the child (ADR-0208 §SD5; the same boundary
// ADR-0134 crossed with a named pipe, widened to a seekable object because
// ffprobe cannot determine a duration from a stream it may not re-read).
//
// A path-shaped input has no reason to go through here; [OpenE] is for that.
type FdInputI interface {
	// Name is what errors and logs call the recording. It is not opened and
	// need not exist.
	Name() string
	// OpenE returns a new read handle on the recording, positioned at the
	// start. Every call must return an independent handle: two decoder
	// processes sharing one open file description would share its offset and
	// seek each other off course. The caller closes what it gets.
	OpenE() (f *os.File, err error)
}

// OpenFfmpegFdE is [OpenFfmpegE] over an [FdInputI]: every ffprobe and ffmpeg
// the source spawns inherits a handle of its own from in and addresses it as
// [childFdPath]. The source owns nothing of in and closes nothing of it; the
// caller keeps it alive for as long as the source is read.
func OpenFfmpegFdE(ctx context.Context, in FdInputI) (inst *FfmpegSource, err error) {
	if in == nil {
		return nil, eb.Build().Errorf("no input")
	}
	format, frames, err := probeFdE(ctx, in)
	if err != nil {
		return nil, err
	}
	err = format.ValidateE()
	if err != nil {
		return nil, eb.Build().Str("path", in.Name()).Errorf("probed format is unusable: %w", err)
	}
	inst = &FfmpegSource{
		ctx:         ctx,
		name:        in.Name(),
		in:          in,
		format:      format,
		frames:      frames,
		bufferBytes: readBufferBytes(format),
		args:        make([]string, 0, 20),
	}
	return inst, nil
}

// ReopenerFd is [Reopener] over an [FdInputI]: each call opens an independent
// ffmpeg source over the same recording, which is what a track's Reopen hands
// the peaks build and the window cache.
func ReopenerFd(in FdInputI) (reopen func(ctx context.Context) (src pcm.SourceI, err error)) {
	return func(ctx context.Context) (src pcm.SourceI, err error) {
		return OpenFfmpegFdE(ctx, in)
	}
}

// childInheritedFd is the descriptor an [exec.Cmd.ExtraFiles] entry lands on
// in the child: entry i becomes 3+i, and there is only ever one entry here.
const childInheritedFd = 3

// childFdPath is how the child names an inherited descriptor. Linux publishes
// them under /proc/self/fd; the BSDs under /dev/fd. Nothing else is supported
// — a host without either has no way to hand a process an unnamed file.
func childFdPath(fd int) (arg string) {
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("/proc/self/fd/%d", fd)
	}
	return fmt.Sprintf("/dev/fd/%d", fd)
}
