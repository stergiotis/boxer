//go:build llm_generated_opus47

package h3

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"unsafe"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/tetratelabs/wazero/api"
)

// Handle is a pool-checked-out wasm module instance. Not safe for
// concurrent use by multiple goroutines. Acquire via [Runtime.AcquireE],
// return via [Handle.Release].
//
// Assumes little-endian host (x86-64, arm64). The reinterpret-cast memory
// helpers rely on the host and wasm target agreeing on byte order;
// wasm-llvm produces little-endian wasm unconditionally, and the repo
// does not target big-endian hosts (see [portability] in CODINGSTANDARDS).
type Handle struct {
	rt  *Runtime
	mod api.Module
	mem api.Memory

	fnExtAlloc      api.Function
	fnExtFree       api.Function
	fnLatLngToCell  api.Function
	fnCellToLatLng  api.Function
	fnCellToParent  api.Function
	fnCellToChildren api.Function
	fnGridDisk      api.Function
	fnCellToString  api.Function
	fnStringToCell  api.Function
	fnAreValid       api.Function
	fnGetResolution  api.Function
	fnPolygonToCells api.Function
	fnCompactCells   api.Function
	fnUncompactCells api.Function

	// scratch is a per-handle region of wasm linear memory, reused across
	// bulk calls to avoid one alloc/free per call per region. It grows
	// geometrically; there is no explicit free — the scratch is reclaimed
	// when the module is closed by [Runtime.Close].
	scratchOff uint32
	scratchCap int

	released atomic.Bool
}

// alignUp8 rounds n up to the next multiple of 8.
func alignUp8(n uint32) uint32 { return (n + 7) &^ 7 }

// ensureScratchE ensures the handle's scratch region has at least n bytes
// of capacity. The returned base offset is stable when n fits in the
// current scratch; otherwise the region is reallocated and any previously
// staged contents are discarded (callers must re-stage inputs after a
// grow).
func (inst *Handle) ensureScratchE(ctx context.Context, n int) (base uint32, err error) {
	if n <= inst.scratchCap {
		base = inst.scratchOff
		return
	}
	newCap := inst.scratchCap * 2
	if newCap < n {
		newCap = n
	}
	if inst.scratchOff != 0 {
		inst.freeNoE(ctx, inst.scratchOff, inst.scratchCap)
		inst.scratchOff = 0
		inst.scratchCap = 0
	}
	var off uint32
	off, err = inst.allocE(ctx, newCap)
	if err != nil {
		return
	}
	inst.scratchOff = off
	inst.scratchCap = newCap
	base = off
	return
}

// Release returns the handle to its Runtime's pool. Idempotent: a
// double-release is silently swallowed so deferred Release()s chained on
// error paths do not panic. The released handle must not be used again
// until re-acquired.
func (inst *Handle) Release() {
	if !inst.released.CompareAndSwap(false, true) {
		return
	}
	if inst.rt.closed.Load() {
		return
	}
	select {
	case inst.rt.pool <- inst:
	default:
		// Pool full — shouldn't happen under normal flow (each pool slot
		// tracks a single handle), but don't block.
	}
}

// --- allocation ---------------------------------------------------------

func (inst *Handle) allocE(ctx context.Context, n int) (off uint32, err error) {
	if n == 0 {
		return
	}
	if n < 0 {
		err = eb.Build().Int("n", n).Errorf("alloc: negative size")
		return
	}
	var results []uint64
	results, err = inst.fnExtAlloc.Call(ctx, uint64(uint32(n)))
	if err != nil {
		err = eh.Errorf("ext_alloc: %w", err)
		return
	}
	off = uint32(results[0])
	if off == 0 {
		err = ErrAllocReturnedZero
	}
	return
}

func (inst *Handle) freeNoE(ctx context.Context, off uint32, n int) {
	if off == 0 || n <= 0 {
		return
	}
	_, _ = inst.fnExtFree.Call(ctx, uint64(off), uint64(uint32(n)))
}

// --- memory helpers -----------------------------------------------------
//
// The zero-copy readers/writers rely on the host being little-endian (see
// the Handle doc comment). The binary.LittleEndian fallbacks exist for
// paths where the zero-copy path is awkward (single scalars, small
// batches) and for documentation of intent.

func (inst *Handle) writeF64sE(off uint32, vals []float64) (err error) {
	if len(vals) == 0 {
		return
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&vals[0])), len(vals)*8)
	if !inst.mem.Write(off, buf) {
		err = eb.Build().Uint32("off", off).Int("n", len(vals)).Errorf("%w", ErrMemoryOOB)
	}
	return
}

func (inst *Handle) readF64sE(off uint32, dst []float64) (err error) {
	if len(dst) == 0 {
		return
	}
	buf, ok := inst.mem.Read(off, uint32(len(dst)*8))
	if !ok {
		err = eb.Build().Uint32("off", off).Int("n", len(dst)).Errorf("%w", ErrMemoryOOB)
		return
	}
	for i := range dst {
		dst[i] = float64FromLE(buf[i*8 : i*8+8])
	}
	return
}

func (inst *Handle) writeU64sE(off uint32, vals []uint64) (err error) {
	if len(vals) == 0 {
		return
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&vals[0])), len(vals)*8)
	if !inst.mem.Write(off, buf) {
		err = eb.Build().Uint32("off", off).Int("n", len(vals)).Errorf("%w", ErrMemoryOOB)
	}
	return
}

func (inst *Handle) readU64sE(off uint32, dst []uint64) (err error) {
	if len(dst) == 0 {
		return
	}
	buf, ok := inst.mem.Read(off, uint32(len(dst)*8))
	if !ok {
		err = eb.Build().Uint32("off", off).Int("n", len(dst)).Errorf("%w", ErrMemoryOOB)
		return
	}
	for i := range dst {
		dst[i] = binary.LittleEndian.Uint64(buf[i*8 : i*8+8])
	}
	return
}

func (inst *Handle) writeI32sE(off uint32, vals []int32) (err error) {
	if len(vals) == 0 {
		return
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&vals[0])), len(vals)*4)
	if !inst.mem.Write(off, buf) {
		err = eb.Build().Uint32("off", off).Int("n", len(vals)).Errorf("%w", ErrMemoryOOB)
	}
	return
}

func (inst *Handle) readI32sE(off uint32, dst []int32) (err error) {
	if len(dst) == 0 {
		return
	}
	buf, ok := inst.mem.Read(off, uint32(len(dst)*4))
	if !ok {
		err = eb.Build().Uint32("off", off).Int("n", len(dst)).Errorf("%w", ErrMemoryOOB)
		return
	}
	for i := range dst {
		dst[i] = int32(binary.LittleEndian.Uint32(buf[i*4 : i*4+4]))
	}
	return
}

func (inst *Handle) writeBytesE(off uint32, buf []byte) (err error) {
	if len(buf) == 0 {
		return
	}
	if !inst.mem.Write(off, buf) {
		err = eb.Build().Uint32("off", off).Int("n", len(buf)).Errorf("%w", ErrMemoryOOB)
	}
	return
}

func (inst *Handle) readBytesE(off uint32, n int) (out []byte, err error) {
	if n == 0 {
		return
	}
	buf, ok := inst.mem.Read(off, uint32(n))
	if !ok {
		err = eb.Build().Uint32("off", off).Int("n", n).Errorf("%w", ErrMemoryOOB)
		return
	}
	out = buf
	return
}

func (inst *Handle) readU32E(off uint32) (v uint32, err error) {
	buf, ok := inst.mem.Read(off, 4)
	if !ok {
		err = eb.Build().Uint32("off", off).Errorf("%w", ErrMemoryOOB)
		return
	}
	v = binary.LittleEndian.Uint32(buf)
	return
}

func (inst *Handle) readStatusE(off uint32, dst []StatusE) (err error) {
	if len(dst) == 0 {
		return
	}
	buf, ok := inst.mem.Read(off, uint32(len(dst)))
	if !ok {
		err = eb.Build().Uint32("off", off).Int("n", len(dst)).Errorf("%w", ErrMemoryOOB)
		return
	}
	for i, b := range buf {
		dst[i] = StatusE(b)
	}
	return
}

// float64FromLE decodes a little-endian IEEE-754 float64 from 8 bytes.
func float64FromLE(b []byte) float64 {
	u := binary.LittleEndian.Uint64(b)
	return *(*float64)(unsafe.Pointer(&u))
}
