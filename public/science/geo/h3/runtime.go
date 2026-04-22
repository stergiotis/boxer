//go:build llm_generated_opus47

package h3

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/geo/h3/internal/h3o_wasm"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// RuntimeConfig configures a [Runtime]. Zero-value-usable:
//   - Zero PoolSize is interpreted as runtime.GOMAXPROCS(0) (minimum 1).
//   - Nil WazeroCfg uses wazero.NewRuntimeConfig() (the compiler backend).
//   - Nil Wasm uses the embedded artifact in
//     public/science/geo/h3/internal/h3o_wasm.
type RuntimeConfig struct {
	PoolSize  int
	WazeroCfg wazero.RuntimeConfig
	Wasm      []byte
}

// Runtime owns a shared wazero runtime and a pool of pre-instantiated
// modules. Safe for concurrent use: callers check out a [Handle] via
// [Runtime.AcquireE] and return it via [Handle.Release].
type Runtime struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	pool     chan *Handle
	closed   atomic.Bool
	closeMu  sync.Mutex
}

func NewRuntime(ctx context.Context, cfg RuntimeConfig) (inst *Runtime, err error) {
	wasmBytes := cfg.Wasm
	if wasmBytes == nil {
		wasmBytes = h3o_wasm.H3Wasm
	}
	if len(wasmBytes) == 0 {
		err = ErrNoWasmBytes
		return
	}
	poolSize := cfg.PoolSize
	if poolSize <= 0 {
		poolSize = runtime.GOMAXPROCS(0)
		if poolSize < 1 {
			poolSize = 1
		}
	}
	wcfg := cfg.WazeroCfg
	if wcfg == nil {
		wcfg = wazero.NewRuntimeConfig()
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wcfg)

	var compiled wazero.CompiledModule
	compiled, err = rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		err = eb.Build().Int("wasmBytes", len(wasmBytes)).Errorf("compile h3 module: %w", err)
		return
	}

	inst = &Runtime{
		rt:       rt,
		compiled: compiled,
		pool:     make(chan *Handle, poolSize),
	}

	for i := 0; i < poolSize; i++ {
		var h *Handle
		h, err = inst.newHandle(ctx, i)
		if err != nil {
			_ = inst.Close()
			err = eb.Build().Int("idx", i).Errorf("instantiate pool slot: %w", err)
			return
		}
		inst.pool <- h
	}
	return
}

func (inst *Runtime) newHandle(ctx context.Context, idx int) (h *Handle, err error) {
	modCfg := wazero.NewModuleConfig().WithName(fmt.Sprintf("h3-%d", idx))
	var mod api.Module
	mod, err = inst.rt.InstantiateModule(ctx, inst.compiled, modCfg)
	if err != nil {
		err = eh.Errorf("instantiate module: %w", err)
		return
	}
	h = &Handle{
		rt:              inst,
		mod:             mod,
		mem:             mod.Memory(),
		fnExtAlloc:      mod.ExportedFunction("ext_alloc"),
		fnExtFree:       mod.ExportedFunction("ext_free"),
		fnLatLngToCell:  mod.ExportedFunction("h3_latlng_to_cell"),
		fnCellToLatLng:  mod.ExportedFunction("h3_cell_to_latlng"),
		fnCellToParent:  mod.ExportedFunction("h3_cell_to_parent"),
		fnCellToChildren: mod.ExportedFunction("h3_cell_to_children"),
		fnGridDisk:      mod.ExportedFunction("h3_grid_disk"),
		fnCellToString:  mod.ExportedFunction("h3_cell_to_string"),
		fnStringToCell:  mod.ExportedFunction("h3_string_to_cell"),
		fnAreValid:      mod.ExportedFunction("h3_are_valid"),
		fnGetResolution: mod.ExportedFunction("h3_get_resolution"),
	}
	{ // Stage: export presence check
		var missing string
		switch {
		case h.fnExtAlloc == nil:
			missing = "ext_alloc"
		case h.fnExtFree == nil:
			missing = "ext_free"
		case h.fnLatLngToCell == nil:
			missing = "h3_latlng_to_cell"
		case h.fnCellToLatLng == nil:
			missing = "h3_cell_to_latlng"
		case h.fnCellToParent == nil:
			missing = "h3_cell_to_parent"
		case h.fnCellToChildren == nil:
			missing = "h3_cell_to_children"
		case h.fnGridDisk == nil:
			missing = "h3_grid_disk"
		case h.fnCellToString == nil:
			missing = "h3_cell_to_string"
		case h.fnStringToCell == nil:
			missing = "h3_string_to_cell"
		case h.fnAreValid == nil:
			missing = "h3_are_valid"
		case h.fnGetResolution == nil:
			missing = "h3_get_resolution"
		}
		if missing != "" {
			_ = mod.Close(ctx)
			err = eb.Build().Str("export", missing).Errorf("%w", ErrExportNotFound)
			return
		}
	}
	return
}

// AcquireE checks a handle out of the pool, blocking until one is available
// or ctx is cancelled. The returned handle is not safe for concurrent use
// and must be returned with [Handle.Release].
func (inst *Runtime) AcquireE(ctx context.Context) (h *Handle, err error) {
	if inst.closed.Load() {
		err = ErrClosed
		return
	}
	select {
	case h = <-inst.pool:
		if h == nil {
			err = ErrClosed
			return
		}
		h.released.Store(false)
	case <-ctx.Done():
		err = eh.Errorf("acquire handle: %w", ctx.Err())
	}
	return
}

// Close tears down every pooled module and the wazero runtime. Callers must
// Release every acquired handle before calling Close; behaviour is undefined
// otherwise. Idempotent.
func (inst *Runtime) Close() (err error) {
	if !inst.closed.CompareAndSwap(false, true) {
		return
	}
	inst.closeMu.Lock()
	defer inst.closeMu.Unlock()
	ctx := context.Background()
	close(inst.pool)
	var first error
	for h := range inst.pool {
		if h == nil || h.mod == nil {
			continue
		}
		e := h.mod.Close(ctx)
		if e != nil && first == nil {
			first = e
		}
	}
	e := inst.rt.Close(ctx)
	if e != nil && first == nil {
		first = e
	}
	if first != nil {
		err = eh.Errorf("close runtime: %w", first)
	}
	return
}
