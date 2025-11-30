//go:build !bootstrap

package application

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/hmi/imzero2/egui"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var CurrentApplication *Application

func resetCurrenApplication() { CurrentApplication = nil }

type Application struct {
	endianess                   binary.ByteOrder
	channel                     *runtime.InlineIoChannel
	fffi                        *runtime.Fffi2
	FffiEstablishedHandler      func(fffi *runtime.Fffi2) error
	BeforeFirstFrameInitHandler func() error
	RenderLoopHandler           func(marshaller *runtime.Marshaller) error
	Config                      *Config
	shutdown                    *bool
	stdout                      *bufio.Writer
	stdin                       *bufio.Reader
	relaunches                  int
	relaunchable                bool
	closers                     []io.Closer
}

func NewApplication(cfg *Config) (app *Application, err error) {
	shutdown := false
	app = &Application{
		channel:                     nil,
		fffi:                        nil,
		FffiEstablishedHandler:      nil,
		BeforeFirstFrameInitHandler: nil,
		RenderLoopHandler:           nil,
		Config:                      cfg,
		shutdown:                    &shutdown,
		stdout:                      nil,
		stdin:                       nil,
		endianess:                   nil,
		relaunchable:                false,
		relaunches:                  0,
		closers:                     nil,
	}
	return
}
func (inst *Application) Launch() (err error) {
	inst.relaunches++
	cfg := inst.Config
	if inst.channel != nil {
		// re-Launch
		if inst.relaunches-1 >= cfg.MaxRelaunches {
			log.Info().Int("relaunches", inst.relaunches).Msg("maximum number of re-launches reached")
			inst.relaunchable = false
			*inst.shutdown = true
			return ErrMaximumNumberOfRelaunches
		} else {
			log.Info().Int("relaunches", inst.relaunches).Int("max", cfg.MaxRelaunches).Msg("re-launching")
		}
	}

	if cfg.UseWasm {
		inst.stdout = bufio.NewWriter(bytes.NewBuffer(make([]byte, 0, 10*1024*104)))
		inst.stdin = bufio.NewReader(bytes.NewBuffer(make([]byte, 0, 10*1024*104)))
		inst.endianess = binary.LittleEndian // wasm uses little endian byte order
		inst.relaunchable = true
		var imguiWasm []byte
		var _ = imguiWasm

		if cfg.ClientBinary == "" {
			// FIXME
			return eh.Errorf("unable to Launch application: no wasm binary given")
		} else {
			imguiWasm, err = os.ReadFile(cfg.ClientBinary)
			if err != nil {
				return eh.Errorf("unable to read wasm file: %w", err)
			}
		}
		/*imzConfig := wazero.NewRuntimeConfigCompiler()
		var imz *wasm.ImZero
		imz, err = wasm.NewImZero(imguiWasm, imzConfig, inst.stdin, inst.stdout, os.Stderr)
		if err != nil {
			return eh.Errorf("unable to create imzero instance: %w", err)
		}
		go func() {
			e := imz.Instantiate()
			if e != nil {
				log.Error().Err(e).Msg("error while running main loop in webassembly binary")
			}
			*inst.shutdown = true
		}()*/
	} else {
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
			inst.relaunchable = false
		} else {
			inst.relaunchable = cfg.MaxRelaunches > 0
			args := make([]string, 0, 32)
			args = append(args, "-fffiInterpreter", "on")
			args = append(args, "-ttfFilePath", cfg.MainFontTTF)
			if inst.Config.ImZeroSkiaClientConfig != nil {
				args = inst.Config.ImZeroSkiaClientConfig.PassthroughArgs(args)
			}
			log.Info().Strs("args", args).Str("binary", cfg.ClientBinary).Msg("launching imzero client")
			var cmd *exec.Cmd
			debugMode := os.Getenv("BOXER_IMZERO_DEBUG_MODE")
			switch debugMode {
			case "":
				cmd = exec.Command(cfg.ClientBinary, args...)
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
			cmd.Stderr = os.Stderr // FIXME log forwarding
			/*se, err = cmd.StderrPipe()
			if err != nil {
				return eb.Build().Str("path",cfg.ImGuiBinary).Errorf("error while getting stderr pipeline: %w", err)
			}*/
			inst.stdout = bufio.NewWriter(si)
			inst.stdin = bufio.NewReader(so)
			err = cmd.Start()
			if err != nil {
				return eb.Build().Str("path", cfg.ClientBinary).Errorf("error while running main loop in external binary: %w", err)
			}
			go func() {
				e := cmd.Wait()

				if e != nil {
					log.Error().Err(e).Str("path", cfg.ClientBinary).Msg("error while running main loop in external binary")
				}
				*inst.shutdown = true
			}()
		}
	}

	if inst.channel != nil {
		// re-Launch
		inst.channel.SetInOut(inst.stdin, inst.stdout)
	} else {
		inst.channel = runtime.NewInlineChannel(inst.stdin,
			inst.stdout,
			inst.endianess,
			inst.handleNonNilError,
			nil)
		fffi := runtime.NewFffi2(inst.channel)
		inst.fffi = fffi
	}
	return
}

var ErrNeedsToBeLaunchedBeforeRun = eh.Errorf("application needs to be launched before run")
var ErrMaximumNumberOfRelaunches = eh.Errorf("maximum number of re-launches reached")

func defaultRenderLoopHandler(marshaller *runtime.Marshaller) error {
	return nil
}

func (inst *Application) Run() (err error) {
	if inst.relaunches == 0 {
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
	fffi := inst.fffi
	marshaller := inst.channel.Marshaller()

	// signal end of configuration phase --> start main loop
	fffi.Flush()

	if inst.RenderLoopHandler == nil {
		inst.RenderLoopHandler = defaultRenderLoopHandler
	}
	defer resetCurrenApplication()
	for !egui.HasErrors() && inst.shouldProceed() {
		CurrentApplication = inst
		marshaller.ResetWrittenBytes()
		err = inst.RenderLoopHandler(marshaller)
		if err != nil {
			inst.handleNonNilError(err)
			err = nil
		}
		fffi.Flush()
	}
	if egui.HasErrors() {
		err = egui.Errors()[0]
		return
	}
	return
}

func (inst *Application) shouldProceed() bool {
	return !*inst.shutdown
}

func (inst *Application) handleNonNilError(err error) {
	if errors.Is(err, io.EOF) {
		*inst.shutdown = true
		err = nil
		return
	}
	if errors.Is(err, syscall.EPIPE) {
		if inst.relaunchable {
			err2 := inst.Launch()
			if err2 != nil {
				log.Error().Err(err).Msg("imzero binary exited, unable to re-Launch")
				*inst.shutdown = true
			} else {
				log.Warn().Err(err).Msg("imzero binary exited, successfully re-launched")
			}
		} else {
			log.Error().Err(err).Msg("imzero binary exited")
			*inst.shutdown = true
		}
	} else {
		if *inst.shutdown == false {
			log.Error().Err(err).Msg("error while communicating through inline channel")
		}
	}
}
