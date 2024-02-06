package wasm

import (
	"context"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
	"golang.org/x/sys/unix"
	"io"
	"time"
)

type ImZero struct {
	ctx       context.Context
	rt        wazero.Runtime
	modConfig wazero.ModuleConfig
	wasm      []byte
	mod       api.Module
	mem       api.Memory
}

func NewImZero(imguiWasm []byte, cfg wazero.RuntimeConfig, stdin io.Reader, stdout io.Writer, stderr io.Writer) (inst *ImZero, err error) {
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)

	// Instantiate WASI
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	startTime := time.Now()
	resMonotonic := unix.Timespec{}
	err = unix.ClockGetres(unix.CLOCK_MONOTONIC, &resMonotonic)
	if err != nil {
		err = eh.Errorf("unable to get clock resolution of monotonic clock: %w", err)
		return
	}
	resWallclock := unix.Timespec{}
	err = unix.ClockGetres(unix.CLOCK_REALTIME, &resWallclock)
	if err != nil {
		err = eh.Errorf("unable to get clock resolution of wall clock: %w", err)
		return
	}

	modConfig := wazero.NewModuleConfig().
		WithStdout(stdout).
		WithStderr(stderr).
		WithStdin(stdin).
		WithNanosleep(func(ns int64) {
			time.Sleep(time.Duration(ns) * time.Nanosecond)
		}).
		WithNanotime(func() int64 {
			return int64(time.Duration(time.Since(startTime)) * time.Nanosecond)
		}, sys.ClockResolution(resMonotonic.Nano())).
		WithWalltime(func() (sec int64, nsec int32) {
			t := time.Now()
			return t.Unix(), int32(t.Nanosecond())
		}, sys.ClockResolution(resWallclock.Nano()))

	inst = &ImZero{
		ctx:       ctx,
		rt:        rt,
		modConfig: modConfig,
		wasm:      imguiWasm,
		mod:       nil,
		mem:       nil,
	}
	return
}
func (inst *ImZero) Instantiate() (err error) {
	var mod api.Module
	mod, err = inst.rt.InstantiateWithConfig(inst.ctx, inst.wasm, inst.modConfig.WithArgs("wasi"))
	if err != nil {
		// Note: Most compilers do not exit the module after running "_start",
		// unless there was an error. This allows you to call exported functions.
		if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() != 0 {
			err = eb.Build().Uint32("exitCode", exitErr.ExitCode()).Errorf("error while running wasi start function: %w", err)
		} else if !ok {
			err = eb.Build().Errorf("error while running wasi start function: %w", err)
			return
		}
	}
	log.Info().Uint32("memorySize", mod.Memory().Size()).Msg("memory size")
	inst.mod = mod
	inst.mem = mod.Memory()

	/*{
		ch := emscripten.NewCallHelper(inst.ctx)
		f := mod.ExportedFunction("example")
		ch.AddBool(true).AddUint8Arg(1).AddUint16(2).AddUint32(3).AddUint64(4).AddInt8(-1).AddInt16(-2).AddInt32(-3).AddInt64(-4)
	}*/
	return
}

func (inst *ImZero) Close() {
	if inst.rt != nil {
		_ = inst.rt.Close(inst.ctx)
		inst.rt = nil
	}
}
