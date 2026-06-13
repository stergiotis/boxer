//go:build !bootstrap

package application

import (
	"bufio"
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
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

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
		switch debugMode {
		case "":
			cmd = exec.Command(cfg.ClientBinary, args...)
			break
		case "flamegraph":
			args = slices.Concat([]string{
				"-o", "flamegraph.svg",
				"--",
				cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with flamegraph (cargo install flamegraph)")
			cmd = exec.Command("flamegraph", args...)
			break
		case "memcheck":
			args = slices.Concat([]string{
				"--leak-check=full",
				"--",
				cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with valgrind memcheck")
			cmd = exec.Command("valgrind", args...)
			break
		case "massif":
			args = slices.Concat([]string{
				"--tool=massif",
				"--threshold=0.1",
				"--",
				cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with valgrind massif")
			cmd = exec.Command("valgrind", args...)
			break
		case "heaptrack":
			args = slices.Concat([]string{cfg.ClientBinary}, args)
			log.Info().Strs("args", args).Msg("starting imzero2 client executable with heaptrack")
			cmd = exec.Command("heaptrack", args...)
			break
		default:
			err = eb.Build().Str("debugMode", debugMode).Strs("possible", []string{"memcheck", "massif", "heaptrack"}).Errorf("unhandled debug mode BOXER_IMZERO_DEBUG_MODE")
			return
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

func defaultRenderLoopHandler() error {
	return nil
}

func (inst *Application[U]) Run() (err error) {
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
	defer func() {
		for _, c := range inst.closers {
			_ = c.Close()
		}
	}()

	if inst.BeforeFirstFrameInitHandler != nil {
		err = inst.BeforeFirstFrameInitHandler()
		if err != nil {
			err = eh.Errorf("BeforeFirstFrameInitHandler returned an error: %w", err)
			return
		}
	}
	marshaller := inst.channel.Marshaller()

	if inst.RenderLoopHandler == nil {
		inst.RenderLoopHandler = defaultRenderLoopHandler
	}
	type byteCountReader interface {
		GetReadBytes() int
		ResetReadBytes()
	}
	unmarshallerCounter, _ := any(inst.unmarshaller).(byteCountReader)
	for !typed.HasErrors() && inst.shouldProceed() {
		marshaller.ResetWrittenBytes()
		if unmarshallerCounter != nil {
			unmarshallerCounter.ResetReadBytes()
		}
		err = inst.RenderLoopHandler()
		if err != nil {
			inst.handleNonNilError(err)
			err = nil
		}
		written := marshaller.GetWrittenBytes()
		read := 0
		if unmarshallerCounter != nil {
			read = unmarshallerCounter.GetReadBytes()
		}
		metrics.Current.RecordBytes(written, read)
	}
	if typed.HasErrors() {
		err = typed.GetError()
		return
	}
	return
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
