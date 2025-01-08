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
	"github.com/tetratelabs/wazero"

	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/nerdfont"
	"github.com/stergiotis/boxer/public/imzero/wasm"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

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
	}
	return
}

var MaximumNumberOfRelaunches = eh.Errorf("maximum number of re-launches reached")

func (inst *Application) Launch() (err error) {
	inst.relaunches++
	cfg := inst.Config
	if inst.channel != nil {
		// re-Launch
		if inst.relaunches-1 >= cfg.MaxRelaunches {
			log.Info().Int("relaunches", inst.relaunches).Msg("maximum number of re-launches reached")
			inst.relaunchable = false
			*inst.shutdown = true
			return MaximumNumberOfRelaunches
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

		if cfg.ImGuiBinary == "" {
			// FIXME
			return eh.Errorf("unable to Launch application: no wasm binary given")
		} else {
			imguiWasm, err = os.ReadFile(cfg.ImGuiBinary)
			if err != nil {
				return eh.Errorf("unable to read wasm file: %w", err)
			}
		}
		imzConfig := wazero.NewRuntimeConfigCompiler()
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
		}()
	} else {
		inst.endianess = binary.NativeEndian

		if cfg.ImGuiBinary == "" {
			inst.stdout = bufio.NewWriter(os.Stdout)
			inst.stdin = bufio.NewReader(os.Stdin)
			inst.relaunchable = false
		} else {
			inst.relaunchable = true
			cmd := exec.Command(cfg.ImGuiBinary)
			var si io.WriteCloser
			var so io.ReadCloser
			//var se io.ReadCloser
			si, err = cmd.StdinPipe()
			if err != nil {
				return eb.Build().Str("path", cfg.ImGuiBinary).Errorf("error while getting stdin pipeline: %w", err)
			}
			so, err = cmd.StdoutPipe()
			if err != nil {
				return eb.Build().Str("path", cfg.ImGuiBinary).Errorf("error while getting stdout pipeline: %w", err)
			}
			/*se, err = cmd.StderrPipe()
			if err != nil {
				return eb.Build().Str("path",cfg.ImGuiBinary).Errorf("error while getting stderr pipeline: %w", err)
			}*/
			inst.stdout = bufio.NewWriter(si)
			inst.stdin = bufio.NewReader(so)
			err = cmd.Start()
			if err != nil {
				return eb.Build().Str("path", cfg.ImGuiBinary).Errorf("error while running main loop in external binary: %w", err)
			}
			go func() {
				e := cmd.Wait()

				if e != nil {
					log.Error().Err(e).Str("path", cfg.ImGuiBinary).Msg("error while running main loop in external binary")
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

var ErrorNeedsToBeLaunchedBeforeRun = eh.Errorf("application needs to be launched before run")

func defaultRenderLoopHandler(marshaller *runtime.Marshaller) error {
	if imgui.Begin("default render loop handler") {
		imgui.TextUnformatted("no render loop handler given!")
	}
	imgui.End()
	return nil
}

func (inst *Application) Run() (err error) {
	if inst.relaunches == 0 {
		return ErrorNeedsToBeLaunchedBeforeRun
	}
	if inst.FffiEstablishedHandler != nil {
		err = inst.FffiEstablishedHandler(inst.fffi)
		if err != nil {
			err = eh.Errorf("FfiEstablishedHandler returned an error: %w", err)
			return
		}
	}
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
	for !imgui.HasErrors() && inst.shouldProceed() {
		marshaller.ResetWrittenBytes()
		err = inst.RenderLoopHandler(marshaller)
		if err != nil {
			inst.handleNonNilError(err)
			//imgui.ShowDemoWindow()
		}
		fffi.Flush()
	}
	if imgui.HasErrors() {
		err = imgui.Errors()[0]
		return
	}
	return
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
		return
	}
	if errors.Is(err, syscall.EPIPE) {
		if inst.relaunchable {
			err2 := inst.Launch()
			if err2 != nil {
				log.Error().Err(err).Msg("imzero binary exited, unable to re-Launch")
				*inst.shutdown = true
			}
			log.Warn().Err(err).Msg("imzero binary exited, sucessfully re-launched")
		} else {
			log.Error().Err(err).Msg("imzero binary exited")
			*inst.shutdown = true
		}
	} else {
		log.Error().Err(err).Msg("error while communicating through inline channel")
	}
}
