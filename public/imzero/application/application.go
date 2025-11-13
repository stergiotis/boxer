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
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/nerdfont"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var CurrentApplication *Application

func resetCurrenApplication() { CurrentApplication = nil }

type PerFrameValues struct {
	DyFontFudge   float32
	LastActiveId  imgui.ImGuiID
	LastHoveredId imgui.ImGuiID
}

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
	IconFont                    imgui.ImFontPtr
	relaunches                  int
	relaunchable                bool
	PerFrameValues              PerFrameValues
	closers                     []io.Closer
}

func NewApplication(cfg *Config) (app *Application, err error) {
	shutdown := false
	app = &Application{
		IconFont:                    0,
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
			cmd := exec.Command(cfg.ClientBinary, args...)
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
	if imgui.Begin("default render loop handler") {
		imgui.TextUnformatted("no render loop handler given!")
	}
	imgui.End()
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
	err = inst.initializeFonts()
	if err != nil {
		err = eh.Errorf("InitializeFonts returned an error: %w", err)
		return
	}

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
	for !imgui.HasErrors() && inst.shouldProceed() {
		CurrentApplication = inst
		marshaller.ResetWrittenBytes()
		err = inst.RenderLoopHandler(marshaller)
		if err != nil {
			inst.handleNonNilError(err)
			err = nil
			//imgui.ShowDemoWindow()
		}
		fffi.Flush()
		inst.populatePerFrameValues()
	}
	if imgui.HasErrors() {
		err = imgui.Errors()[0]
		return
	}
	return
}
func (inst *Application) populatePerFrameValues() {
	inst.PerFrameValues.DyFontFudge = imgui.GetSkiaFontDyFudge()
	inst.PerFrameValues.LastHoveredId, inst.PerFrameValues.LastActiveId = imgui.GetIdPreviousFrame()
}

func (inst *Application) initializeFonts() (err error) {
	cfg := inst.Config
	if cfg.MainFontTTF != "" {
		var ttf []byte
		ttf, err = os.ReadFile(cfg.MainFontTTF)
		if err != nil {
			return eh.Errorf("unable to read main font ttf file: %w", err)
		}
		glyphRanges := make([]imgui.ImWchar, 0, 3)
		glyphRanges = append(glyphRanges, 0x0020) // FIXME can not start with 0 (termination)?
		glyphRanges = append(glyphRanges, imgui.ImWchar(nerdfont.MaxCodepoint))
		//glyphRanges = append(glyphRanges, 0x00ff)
		glyphRanges = append(glyphRanges, 0)
		fc := imgui.NewFontConfig()
		fc.FontData = ttf
		fc.GlyphRanges = glyphRanges
		fc.Name = cfg.MainFontTTF
		_, err = imgui.AddFont(fc, cfg.MainFontSizeInPixels)
		if err != nil {
			return eh.Errorf("unable to add font %w", err)
		}
		fc2 := imgui.NewFontConfig()
		fc2.FontData = ttf
		fc2.GlyphRanges = glyphRanges
		fc2.Name = "icons" //cfg.MainFontTTF + " (icon size)"
		inst.IconFont, err = imgui.AddFont(fc2, cfg.MainFontSizeInPixels*2)
		if err != nil {
			return eh.Errorf("unable to add font %w", err)
		}
	} else {
		inst.IconFont = 0
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
