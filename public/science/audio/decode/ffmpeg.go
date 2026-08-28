package decode

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

const (
	// readAheadMillis is how much audio the stdout buffer holds — long enough
	// that the pipe is read in few syscalls, short enough that a killed
	// process throws little away.
	readAheadMillis int64 = 200
	// readBufferMinBytes and readBufferMaxBytes bound the buffer so neither a
	// telephone-rate mono file nor a 32-channel 384 kHz one picks an absurd
	// size.
	readBufferMinBytes int = 64 << 10
	readBufferMaxBytes int = 1 << 20
	// stderrTailBytes is how much of ffmpeg's diagnostics is kept for the
	// error message.
	stderrTailBytes int = 4 << 10
)

// FfmpegSource decodes any format ffmpeg reads into interleaved float32
// frames, by streaming `-f f32le` out of one ffmpeg process (ADR-0208 §SD5).
//
// The process is a stream: reads at the position it has reached continue it,
// and a read anywhere else restarts it at the new offset — see
// [FfmpegSource.Restarts] and the package documentation. The declared frame
// count from ffprobe is delivered exactly, padding with silence what the codec
// falls short of; see [FfmpegSource.Padded].
//
// One goroutine reads at a time, as [pcm.SourceI] requires. The counters are
// atomic so a UI or a test can sample them from another goroutine.
type FfmpegSource struct {
	// ctx bounds the ffmpeg process, including the processes later restarts
	// spawn, which is why it is held rather than taken per read: a source
	// outlives any one read and the process must not.
	ctx    context.Context
	path   string
	format pcm.Format
	frames int64

	bufferBytes int
	args        []string

	cmd    *exec.Cmd
	reader *bufio.Reader
	stderr *stderrTail
	asm    f32Assembler

	// pos is the frame the running process will deliver next; a read there
	// continues the stream.
	pos int64
	// eos records that the running process reached the end of its output
	// before the declared frame count.
	eos      bool
	closed   bool
	restarts atomic.Int64
	padded   atomic.Int64
}

var _ pcm.SourceI = (*FfmpegSource)(nil)

// OpenFfmpegE probes path with ffprobe and returns a source ready to read; the
// decoder process starts on the first read. ctx bounds every process the
// source spawns, so cancelling it ends playback and any build reading through
// this source.
func OpenFfmpegE(ctx context.Context, path string) (inst *FfmpegSource, err error) {
	format, frames, err := probeE(ctx, path)
	if err != nil {
		return nil, err
	}
	err = format.ValidateE()
	if err != nil {
		return nil, eb.Build().Str("path", path).Errorf("probed format is unusable: %w", err)
	}
	inst = &FfmpegSource{
		ctx:         ctx,
		path:        path,
		format:      format,
		frames:      frames,
		bufferBytes: readBufferBytes(format),
		args:        make([]string, 0, 20),
	}
	return inst, nil
}

// Format implements [pcm.SourceI]; it is the probed rate and channel count,
// which is also what ffmpeg is asked to output, so no resampling or downmix
// happens on the way (a file with more than two channels is decoded as it is).
func (inst *FfmpegSource) Format() (format pcm.Format) { return inst.format }

// Frames implements [pcm.SourceI]. It is the count ffprobe declared and the
// count the source delivers, whatever the codec produces.
func (inst *FfmpegSource) Frames() (frames int64) { return inst.frames }

// DeclaredFrames is round(duration × sample rate) as ffprobe reported it,
// which is what [FfmpegSource.Frames] returns: the declared length is the
// contract. Read together with [FfmpegSource.Padded] it says how far the
// codec's own output fell short of its container's duration.
func (inst *FfmpegSource) DeclaredFrames() (frames int64) { return inst.frames }

// Restarts counts how often a read at an offset other than the stream's
// position tore the decoder down and started a new one. It is a diagnostic:
// a consumer seeing it climb wants its own decoder ([Reopener]) or a window
// cache above this one (ADR-0208 §SD3), not a faster restart.
func (inst *FfmpegSource) Restarts() (n int64) { return inst.restarts.Load() }

// Padded counts frames that were reported as silence because ffmpeg's output
// ended before the declared frame count. A handful at the end of a lossy file
// is expected; a large count means the container's duration and its payload
// disagree.
func (inst *FfmpegSource) Padded() (n int64) { return inst.padded.Load() }

// ReadFramesAtE implements [pcm.SourceI]. A read at the stream's current
// position continues it; any other offset restarts the process there, which
// costs a spawn plus ffmpeg's decode-and-discard up to the seek point.
//
// ctx is checked before the first chunk and between chunks, so a cancellation
// is observed within one buffer of audio; the process itself dies with the
// context given to [OpenFfmpegE].
func (inst *FfmpegSource) ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error) {
	if inst.closed {
		return 0, eb.Build().Str("path", inst.path).Errorf("read from a closed source")
	}
	want, err := pcm.ClampReadE(inst.format, inst.frames, frameOffset, dst)
	if err != nil || want == 0 {
		return want, err
	}
	err = ctx.Err()
	if err != nil {
		return 0, eb.Build().Str("path", inst.path).Errorf("read cancelled: %w", err)
	}
	channels := int(inst.format.Channels)
	samples := want * channels

	if frameOffset != inst.pos || (inst.cmd == nil && !inst.eos) {
		err = inst.startAtE(frameOffset)
		if err != nil {
			return 0, err
		}
	}

	filled := 0
	for filled < samples {
		err = ctx.Err()
		if err != nil {
			inst.stop()
			return 0, eb.Build().Str("path", inst.path).Errorf("read cancelled: %w", err)
		}
		if inst.asm.Pending() >= bytesPerSample {
			filled += inst.asm.Decode(nil, dst[filled:samples])
			continue
		}
		if inst.eos {
			break
		}
		need := (samples-filled)*bytesPerSample - inst.asm.Pending()
		avail := inst.reader.Buffered()
		if avail == 0 {
			_, perr := inst.reader.Peek(1)
			avail = inst.reader.Buffered()
			if avail == 0 {
				err = inst.endStreamE(perr)
				if err != nil {
					// A stream that failed must not read as silence on the
					// next call, so the source is left ready to start over
					// rather than marked ended.
					inst.eos = false
					return 0, err
				}
				continue
			}
		}
		chunk, _ := inst.reader.Peek(min(avail, need))
		filled += inst.asm.Decode(chunk, dst[filled:samples])
		_, _ = inst.reader.Discard(len(chunk))
	}

	if filled < samples {
		// The codec produced fewer frames than the container's duration
		// declared. The declared length is what consumers preallocated for,
		// so the shortfall reads as silence rather than as a short read; a
		// stream that stopped mid-frame has that frame completed too.
		inst.padded.Add(int64(want - filled/channels))
		clear(dst[filled:samples])
	}
	inst.pos += int64(want)
	return want, nil
}

// CloseE implements [pcm.SourceI]: it kills the decoder, reaps it and closes
// its pipes. It is idempotent, and reading after it is an error.
func (inst *FfmpegSource) CloseE() (err error) {
	if inst.closed {
		return nil
	}
	inst.closed = true
	inst.stop()
	inst.asm.Reset()
	return nil
}

// startAtE tears down whatever stream is open and starts a decoder that
// delivers frameOffset first. A stream that existed — running or ended — makes
// this a restart and is counted.
func (inst *FfmpegSource) startAtE(frameOffset int64) (err error) {
	restart := inst.cmd != nil || inst.eos
	inst.stop()
	// From here there is no stream: a failure below must leave the source
	// wanting a start, not reading the old one's end as silence.
	inst.eos = false

	inst.args = inst.appendArgs(inst.args[:0], frameOffset)
	cmd, err := extbin.Ffmpeg.Command(inst.ctx, extbin.Opts{}, inst.args...)
	if err != nil {
		return eb.Build().Str("path", inst.path).Errorf("unable to resolve ffmpeg: %w", err)
	}
	tail := newStderrTail(stderrTailBytes)
	cmd.Stderr = tail
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return eb.Build().Str("path", inst.path).Errorf("unable to pipe ffmpeg stdout: %w", err)
	}
	err = cmd.Start()
	if err != nil {
		return eb.Build().Str("path", inst.path).Errorf("unable to start ffmpeg: %w", err)
	}
	if inst.reader == nil {
		inst.reader = bufio.NewReaderSize(stdout, inst.bufferBytes)
	} else {
		inst.reader.Reset(stdout)
	}
	inst.cmd = cmd
	inst.stderr = tail
	inst.asm.Reset()
	inst.pos = frameOffset
	if restart {
		inst.restarts.Add(1)
	}
	return nil
}

// appendArgs builds the decode invocation. -ss goes before -i, where ffmpeg's
// seek is accurate: it decodes and discards from the nearest seek point up to
// the requested instant itself, so the first sample out is the one at
// frameOffset.
func (inst *FfmpegSource) appendArgs(dst []string, frameOffset int64) (out []string) {
	out = append(dst, "-nostdin", "-v", "error")
	if frameOffset > 0 {
		seconds := float64(frameOffset) / float64(inst.format.SampleRate)
		out = append(out, "-ss", strconv.FormatFloat(seconds, 'f', 9, 64))
	}
	out = append(out,
		"-i", inst.path,
		"-map", "0:a:0",
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", strconv.FormatUint(uint64(inst.format.Channels), 10),
		"-ar", strconv.FormatUint(uint64(inst.format.SampleRate), 10),
		"-")
	return out
}

// endStreamE marks the stream ended and reaps the process, folding its exit
// status and the tail of its stderr into the error. cause is what the pipe
// reported; io.EOF is the normal end.
func (inst *FfmpegSource) endStreamE(cause error) (err error) {
	inst.eos = true
	cmd := inst.cmd
	inst.cmd = nil
	if cmd != nil {
		waitErr := cmd.Wait()
		if waitErr != nil {
			return eb.Build().
				Str("path", inst.path).
				Str("stderr", inst.stderrText()).
				Errorf("ffmpeg exited with an error: %w", waitErr)
		}
	}
	if cause != nil && !errors.Is(cause, io.EOF) {
		return eb.Build().
			Str("path", inst.path).
			Str("stderr", inst.stderrText()).
			Errorf("unable to read ffmpeg output: %w", cause)
	}
	return nil
}

// stderrText is the tail of the current decoder's diagnostics, empty when no
// decoder has been started.
func (inst *FfmpegSource) stderrText() (s string) {
	if inst.stderr == nil {
		return ""
	}
	return inst.stderr.String()
}

// stop kills the decoder and reaps it. A killed process's exit status is not
// news, so it is discarded; cmd.Wait closes the stdout pipe.
func (inst *FfmpegSource) stop() {
	cmd := inst.cmd
	inst.cmd = nil
	if cmd == nil {
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

// readBufferBytes sizes the stdout buffer at readAheadMillis of the stream,
// within the fixed bounds.
func readBufferBytes(format pcm.Format) (n int) {
	perSecond := int64(format.SampleRate) * int64(format.Channels) * int64(bytesPerSample)
	n = int(min(perSecond*readAheadMillis/1000, int64(readBufferMaxBytes)))
	return max(n, readBufferMinBytes)
}

// stderrTail keeps the last max bytes written to it. ffmpeg's diagnostics are
// worth reporting and a broken input must not turn them into unbounded
// memory. exec writes from a goroutine that cmd.Wait joins, while the error
// paths read the tail on either side of that join, so the mutex is load
// bearing.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newStderrTail(maxBytes int) (inst *stderrTail) {
	return &stderrTail{buf: make([]byte, 0, maxBytes), max: maxBytes}
}

func (inst *stderrTail) Write(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = len(p)
	if n >= inst.max {
		inst.buf = append(inst.buf[:0], p[n-inst.max:]...)
		return n, nil
	}
	if drop := len(inst.buf) + n - inst.max; drop > 0 {
		inst.buf = inst.buf[:copy(inst.buf, inst.buf[drop:])]
	}
	inst.buf = append(inst.buf, p...)
	return n, nil
}

func (inst *stderrTail) String() (s string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return strings.TrimSpace(string(inst.buf))
}
