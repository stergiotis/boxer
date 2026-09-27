//go:build !bootstrap

package application

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync/atomic"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// imzero2Client is the render-client binary launched over the FFI transport.
// Its path is caller-supplied (Config.ClientBinary), so it is a Local program;
// the profiler wrappers below (flamegraph/valgrind/heaptrack) are Host tools
// that re-exec that same client path.
var imzero2Client = extbin.Declare(extbin.Program{
	Name:        "imzero2-client",
	Kind:        extbin.Local,
	InstallHint: "build the imzero2 client and point Config.ClientBinary at it",
})

type Application[U runtime.UnmarshallReaderI] struct {
	endianess                   binary.ByteOrder
	channel                     *runtime.InlineIoChannel[U]
	fffi                        *runtime.Fffi2[U]
	FffiEstablishedHandler      func(fffi *runtime.Fffi2[U]) error
	BeforeFirstFrameInitHandler func() error
	RenderLoopHandler           func() error
	Config                      *Config
	shutdown                    atomic.Bool
	stdout                      *bufio.Writer
	stdin                       *bufio.Reader
	closers                     []io.Closer
	unmarshaller                U
}

func NewApplication[U runtime.UnmarshallReaderI](cfg *Config, unmarshaller U) (app *Application[U], err error) {
	app = &Application[U]{
		channel:                     nil,
		fffi:                        nil,
		FffiEstablishedHandler:      nil,
		BeforeFirstFrameInitHandler: nil,
		RenderLoopHandler:           nil,
		Config:                      cfg,
		shutdown:                    atomic.Bool{},
		stdout:                      nil,
		stdin:                       nil,
		endianess:                   nil,
		closers:                     nil,
		unmarshaller:                unmarshaller,
	}
	return
}
func (inst *Application[U]) Launch() (err error) {
	cfg := inst.Config
	inst.endianess = binary.NativeEndian

	if cfg.ClientBinary == "" {
		var in io.Reader
		var out io.Writer
		if cfg.ImZeroCmdInFile != "" {
			var f *os.File
			f, err = os.OpenFile(cfg.ImZeroCmdInFile, os.O_RDONLY, os.ModePerm)
			if err != nil {
				err = eb.Build().Str("path", cfg.ImZeroCmdInFile).Errorf("unable to open imZeroCmdInFile for reading: %w", err)
				return
			}
			inst.closers = append(inst.closers, f)
			in = f
			log.Info().Str("imZeroCmdInFile", cfg.ImZeroCmdInFile).Msg("using file for imzero ipc")
		} else {
			in = os.Stdin
		}
		if cfg.ImZeroCmdOutFile != "" {
			var f *os.File
			f, err = os.OpenFile(cfg.ImZeroCmdOutFile, os.O_WRONLY, os.ModePerm)
			if err != nil {
				err = eb.Build().Str("path", cfg.ImZeroCmdOutFile).Errorf("unable to open imZeroCmdOutFile for writing: %w", err)
				return
			}
			inst.closers = append(inst.closers, f)
			out = f
			log.Info().Str("imZeroCmdOutFile", cfg.ImZeroCmdInFile).Msg("using file for imzero ipc")
		} else {
			out = os.Stdout
		}
		inst.stdout = bufio.NewWriter(out)
		inst.stdin = bufio.NewReader(in)
	} else {
		args := make([]string, 0, 32)
		args = append(args, "imzero2")
		if cfg.MainFontTTF != "" {
			args = append(args, "-mainFontTTF", cfg.MainFontTTF)
		}
		if cfg.MonoFontTTF != "" {
			args = append(args, "-monoFontTTF", cfg.MonoFontTTF)
		}
		if cfg.PhosphorFontTTF != "" {
			args = append(args, "-phosphorFontTTF", cfg.PhosphorFontTTF)
		}
		if cfg.FallbackFontTTF != "" {
			args = append(args, "-fallbackFontTTF", cfg.FallbackFontTTF)
		}
		if cfg.MainFontSizeInPixels > 0 {
			args = append(args, "-mainFontSizeInPixels", fmt.Sprintf("%g", cfg.MainFontSizeInPixels))
		}
		addTweak := func(prefix string, tw FontTweakConfig) {
			if tw.Scale != 0 && tw.Scale != 1.0 {
				args = append(args, "-"+prefix+"Scale", fmt.Sprintf("%g", tw.Scale))
			}
			if tw.YOffsetFactor != 0 {
				args = append(args, "-"+prefix+"YOffsetFactor", fmt.Sprintf("%g", tw.YOffsetFactor))
			}
			if tw.YOffset != 0 {
				args = append(args, "-"+prefix+"YOffset", fmt.Sprintf("%g", tw.YOffset))
			}
		}
		addTweak("mainFont", cfg.MainFontTweak)
		addTweak("monoFont", cfg.MonoFontTweak)
		addTweak("phosphorFont", cfg.PhosphorFontTweak)
		addTweak("fallbackFont", cfg.FallbackFontTweak)
		if inst.Config.ImZeroSkiaClientConfig != nil {
			args = inst.Config.ImZeroSkiaClientConfig.PassthroughArgs(args)
		}
		log.Info().Strs("args", args).Str("binary", cfg.ClientBinary).Msg("launching imzero client")
		var cmd *exec.Cmd
		debugMode := imzero2env.DebugMode.Get()
		// context.Background(): the client is long-lived and its lifetime is
		// managed via the FFI pipe EOF handshake + background cmd.Wait below,
		// not by any spawn context. extbin only resolves the binary here.
		switch debugMode {
		case "":
			cmd, err = imzero2Client.Command(context.Background(), extbin.Opts{Path: cfg.ClientBinary}, args...)
			break
		case "flamegraph":
			args = slices.Concat([]string{
				"-o", "flamegraph.svg",
				"--",
				cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with flamegraph (cargo install flamegraph)")
			cmd, err = extbin.Flamegraph.Command(context.Background(), extbin.Opts{}, args...)
			break
		case "memcheck":
			args = slices.Concat([]string{
				"--leak-check=full",
				"--",
				cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with valgrind memcheck")
			cmd, err = extbin.Valgrind.Command(context.Background(), extbin.Opts{}, args...)
			break
		case "massif":
			args = slices.Concat([]string{
				"--tool=massif",
				"--threshold=0.1",
				"--",
				cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with valgrind massif")
			cmd, err = extbin.Valgrind.Command(context.Background(), extbin.Opts{}, args...)
			break
		case "heaptrack":
			args = slices.Concat([]string{cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with heaptrack")
			cmd, err = extbin.Heaptrack.Command(context.Background(), extbin.Opts{}, args...)
			break
		default:
			err = eb.Build().Str("debugMode", debugMode).Strs("possible", []string{"memcheck", "massif", "heaptrack"}).Errorf("unhandled debug mode BOXER_IMZERO_DEBUG_MODE")
			return
		}
		if err != nil {
			return eb.Build().Str("debugMode", debugMode).Str("binary", cfg.ClientBinary).Errorf("resolve imzero client binary: %w", err)
		}
		var si io.WriteCloser
		var so io.ReadCloser
		//var se io.ReadCloser
		si, err = cmd.StdinPipe()
		if err != nil {
			return eb.Build().Str("path", cfg.ClientBinary).Errorf("error while getting stdin pipeline: %w", err)
		}
		so, err = cmd.StdoutPipe()
		if err != nil {
			return eb.Build().Str("path", cfg.ClientBinary).Errorf("error while getting stdout pipeline: %w", err)
		}
		cmd.Stderr = os.Stderr
		inst.stdout = bufio.NewWriter(si)
		inst.stdin = bufio.NewReader(so)
		err = cmd.Start()
		if err != nil {
			return eb.Build().Str("path", cfg.ClientBinary).Errorf("error while running main loop in external binary: %w", err)
		}
		go func() {
			e := cmd.Wait()
			// Set shutdown before logging so a concurrent render-loop
			// read/write that's racing with cmd.Wait() observes
			// shutdown==true and short-circuits silently in
			// handleNonNilError instead of falling through to a
			// spurious "error while communicating" entry.
			inst.shutdown.Store(true)
			if e != nil {
				log.Error().Err(e).Str("path", cfg.ClientBinary).Msg("imzero binary exited abnormally")
			} else {
				log.Info().Str("path", cfg.ClientBinary).Msg("imzero binary exited cleanly")
			}
		}()
	}

	inst.channel = runtime.NewInlineIoChannel[U](inst.unmarshaller,
		inst.stdin,
		inst.stdout,
		inst.endianess,
		inst.handleNonNilError,
		nil)
	inst.fffi = runtime.NewFffi2[U](inst.channel)
	return
}

var ErrNeedsToBeLaunchedBeforeRun = eh.Errorf("application needs to be launched before run")

// Channel is the FFFI2 transport Launch established, or nil before Launch.
func (inst *Application[U]) Channel() *runtime.InlineIoChannel[U] {
	return inst.channel
}

func defaultRenderLoopHandler() error {
	return nil
}

// Begin does everything Run does before its first frame: the established
// and first-frame handlers, the render-goroutine binding, the default loop
// handler. Run is Begin, Step until the loop should stop, and the closers;
// a host that owns the frame cadence — a browser worker calling a frame
// export per tick (ADR-0077 O2) — calls the three itself.
func (inst *Application[U]) Begin() (err error) {
	if inst.channel == nil {
		return ErrNeedsToBeLaunchedBeforeRun
	}
	if inst.FffiEstablishedHandler != nil {
		err = inst.FffiEstablishedHandler(inst.fffi)
		if err != nil {
			err = eh.Errorf("FfiEstablishedHandler returned an error: %w", err)
			return
		}
	}
	if imzero2env.RenderGoroutineCheck.Get() {
		inst.fffi.BindToCurrentGoroutine()
		log.Info().Msg("imzero2: FFFI channel bound to the render goroutine; a call from any other goroutine panics")
	}
	if inst.BeforeFirstFrameInitHandler != nil {
		err = inst.BeforeFirstFrameInitHandler()
		if err != nil {
			err = eh.Errorf("BeforeFirstFrameInitHandler returned an error: %w", err)
			return
		}
	}
	if inst.RenderLoopHandler == nil {
		inst.RenderLoopHandler = defaultRenderLoopHandler
	}
	return
}

// Step runs one frame: the render loop handler and the byte accounting.
// more is false once the loop should stop (Shutdown, or an error the channel
// recorded); err carries that error.
func (inst *Application[U]) Step() (more bool, err error) {
	if typed.HasErrors() || !inst.shouldProceed() {
		if typed.HasErrors() {
			err = typed.GetError()
		}
		return false, err
	}
	type byteCountReader interface {
		GetReadBytes() int
		ResetReadBytes()
	}
	marshaller := inst.channel.Marshaller()
	unmarshallerCounter, _ := any(inst.unmarshaller).(byteCountReader)
	marshaller.ResetWrittenBytes()
	if unmarshallerCounter != nil {
		unmarshallerCounter.ResetReadBytes()
	}
	yieldToOtherGoroutines()
	if e := inst.RenderLoopHandler(); e != nil {
		inst.handleNonNilError(e)
	}
	yieldToOtherGoroutines()
	read := 0
	if unmarshallerCounter != nil {
		read = unmarshallerCounter.GetReadBytes()
	}
	metrics.Current.RecordBytes(marshaller.GetWrittenBytes(), read)
	return true, nil
}

// CloseAll runs the closers Launch registered; Run does it on return.
func (inst *Application[U]) CloseAll() {
	for _, c := range inst.closers {
		_ = c.Close()
	}
}

func (inst *Application[U]) Run() (err error) {
	if err = inst.Begin(); err != nil {
		return
	}
	defer inst.CloseAll()
	for {
		more, e := inst.Step()
		if !more {
			err = e
			return
		}
	}
}

// Shutdown asks the render loop to stop at the next frame boundary, so that
// Run returns normally and its caller's deferred cleanup runs. It is safe to
// call from any goroutine — including a signal handler — and is idempotent.
//
// It does not preempt an in-flight frame: a loop blocked reading from a wedged
// client will not observe this. A caller that must bound the wait (a signal
// handler, a supervisor) needs its own escalation path.
func (inst *Application[U]) Shutdown() {
	inst.shutdown.Store(true)
}

func (inst *Application[U]) shouldProceed() bool {
	return !inst.shutdown.Load()
}

func (inst *Application[U]) handleNonNilError(err error) {
	if inst.shutdown.Load() {
		return
	}
	// All four of these are expected during shutdown — io.EOF on the next
	// read after Rust closed its writer, syscall.EPIPE on the next write
	// after Rust closed its reader, and os.ErrClosed once exec.Cmd's
	// finalizer has actually called Close() on the StdinPipe/StdoutPipe
	// ends after cmd.Wait() returned. The cmd.Wait() goroutine logs the
	// actual exit status; here we just stop the render loop.
	if errors.Is(err, io.EOF) || errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed) {
		inst.shutdown.Store(true)
		return
	}
	log.Error().Err(err).Msg("error while communicating through inline channel")
}
