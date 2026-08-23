package typed

import (
	"bytes"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"unique"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/zeebo/xxh3"
)

func (inst *RetainedFffiHolder) GetRetainedElementId() (id RetainedElementId) {
	id = inst.retainedElementId
	return
}

// Content returns the raw opcode bytes held by this retained holder. Intended
// for tests and low-level introspection; callers must not mutate the returned
// slice as its backing storage is interned and shared.
func (inst *RetainedFffiHolder) Content() (ret []byte) {
	ret = inst.content
	return
}
func NewRetainedFffiHolderTyped[T any](r *RetainedFffiHolder) RetainedFffiHolderTyped[T] {
	return RetainedFffiHolderTyped[T]{
		interned:          r.interned,
		content:           r.content,
		retainedElementId: r.retainedElementId,
		widgetIdOffset:    r.widgetIdOffset,
	}
}
func (inst RetainedFffiHolderTyped[T]) Untype() *RetainedFffiHolder {
	return &RetainedFffiHolder{
		interned:          inst.interned,
		content:           inst.content,
		retainedElementId: inst.retainedElementId,
		widgetIdOffset:    inst.widgetIdOffset,
	}
}

// builderSizeHint is the smoothed high-water mark of the wire bytes a single
// [RetainedFffiBuilder] carries, and with it the ADAPTIVE retention ceiling
// [RetainedFffiBuilder.putInPool] applies (runtime.PooledBufferCeiling: twice
// the hint, clamped to [runtime.PooledCeilingMin, runtime.PooledCeilingMax]).
//
// A ceiling is insurance against an outlier, not a sizing mechanism; put it
// under the steady state and it becomes the bug it was meant to prevent. This
// pool has now been there twice with a FIXED value — at 4 KiB (ADR-0049
// Update 2026-08-18, where bytes.growSlice was 47.8% of all bytes allocated),
// and again at 256 KiB, which a 2026-08-23 profile caught discarding the dock
// area's ~400 KiB spliced block map every frame. A ceiling that tracks the
// working set cannot drift under it a third time.
//
// Only builders at or above builderHintParticipationFloor fold into the hint.
// The population here is overwhelmingly small widget builders — a profile put
// acquisitions at ~200k/s — and folding every one of them would drag the hint
// to their size, pay an atomic write per opcode to do it, and leave the
// ceiling pinned at the minimum. The buffers a retention ceiling is about are
// the big ones, so they are the ones that get a vote.
var builderSizeHint atomic.Uint64

// builderHintParticipationFloor is the payload size at which a builder starts
// folding into builderSizeHint. Set to the smallest payload that could ever
// be at risk of the ceiling — PooledCeilingMin / slack — so no builder that
// the ceiling could drop is excluded from the hint that sets it.
//
// Consequence worth naming: a hint only decays when something folds into it,
// so when the big builders stop appearing at all (the dock tab that produced
// them is closed) the hint freezes at its last value rather than decaying
// back. The ceiling it implies then stays high for the process's lifetime.
// That is deliberate — a ceiling too high only widens what the pool is
// allowed to keep, and PooledCeilingMax plus sync.Pool's collector-driven
// clearing still bound the retention — but it does mean this hint is not a
// reading of the CURRENT working set the way a ScopeHint's is.
const builderHintParticipationFloor = runtime.PooledCeilingMin / 2

// defaultBufferSize is the capacity of a *fresh* buffer — pool misses only,
// which are rare (~0.06% of acquisitions in the same measurement). It is
// deliberately small and deliberately NOT derived from the ceiling:
// the many small widget builders would otherwise each start at a fraction of
// the ceiling, paying the outliers' size on every miss.
const defaultBufferSize = 512

var retainedFffiBuilderPool = sync.Pool{New: func() any {
	return newRetainedFffiBuilderFresh()
}}

func (inst *RetainedFffiBuilder) WriteOpCode(code uint32) {
	inst.builder.marshaller.WriteUint32(code)
}
func (inst *RetainedFffiBuilder) SpliceRetained(r *RetainedFffiHolder) {
	_, err := inst.builder.buf.Write(r.content)
	if err != nil {
		log.Panic().Err(err).Msg("unable to write to builder")
	}
}
func (inst *RetainedFffiBuilder) WriteUint8(v uint8) {
	inst.builder.marshaller.WriteUint8(v)
}

func (inst *RetainedFffiBuilder) WriteBool(v bool) {
	inst.builder.marshaller.WriteBool(v)
}

func (inst *RetainedFffiBuilder) WriteUint16(v uint16) {
	inst.builder.marshaller.WriteUint16(v)
}

func (inst *RetainedFffiBuilder) WriteUint32(v uint32) {
	inst.builder.marshaller.WriteUint32(v)
}

func (inst *RetainedFffiBuilder) WriteUint64(v uint64) {
	inst.builder.marshaller.WriteUint64(v)
}

func (inst *RetainedFffiBuilder) WriteInt8(v int8) {
	inst.builder.marshaller.WriteInt8(v)
}

func (inst *RetainedFffiBuilder) WriteInt16(v int16) {
	inst.builder.marshaller.WriteInt16(v)
}

func (inst *RetainedFffiBuilder) WriteInt32(v int32) {
	inst.builder.marshaller.WriteInt32(v)
}

func (inst *RetainedFffiBuilder) WriteInt64(v int64) {
	inst.builder.marshaller.WriteInt64(v)
}

func (inst *RetainedFffiBuilder) WriteFloat32(v float32) {
	inst.builder.marshaller.WriteFloat32(v)
}

func (inst *RetainedFffiBuilder) WriteFloat64(v float64) {
	inst.builder.marshaller.WriteFloat64(v)
}

func (inst *RetainedFffiBuilder) WriteComplex64(v complex64) {
	inst.builder.marshaller.WriteComplex64(v)
}

func (inst *RetainedFffiBuilder) WriteComplex128(v complex128) {
	inst.builder.marshaller.WriteComplex128(v)
}

func (inst *RetainedFffiBuilder) WriteString(v string) {
	inst.builder.marshaller.WriteString(v)
}

func (inst *RetainedFffiBuilder) WriteBytes(v []byte) {
	inst.builder.marshaller.WriteBytes(v)
}

func (inst *RetainedFffiBuilder) WriteSliceLength(l int) {
	inst.builder.marshaller.WriteSliceLength(l)
}

func (inst *RetainedFffiBuilder) WriteNilSlice() {
	inst.builder.marshaller.WriteNilSlice()
}

var _ runtime.MarshallWriterI = (*RetainedFffiBuilder)(nil)

type retainedFffiBuilderPooled struct {
	buf        *bytes.Buffer
	marshaller *runtime.Marshaller
}

func errorHandler(err error) {
	log.Warn().Err(err).Msg("error while writing to marshaller")
}

func newRetainedFffiBuilderFresh() *retainedFffiBuilderPooled {
	builder := bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
	return &retainedFffiBuilderPooled{
		buf:        builder,
		marshaller: runtime.NewMarshaller(builder, binary.LittleEndian, errorHandler),
	}
}
func NewRetainedFffiBuilder() *RetainedFffiBuilder {
	return newRetainedFffiBuilderPooled()
}
func newRetainedFffiBuilderPooled() (inst *RetainedFffiBuilder) {
	inst = &RetainedFffiBuilder{
		builder: retainedFffiBuilderPool.Get().(*retainedFffiBuilderPooled),
	}
	err := inst.initialize()
	if compiletimeflags.ExtraChecks && err != nil {
		log.Panic().Err(err).Msg("unable to initialize retained fffi builder")
	}
	return
}
func (inst *RetainedFffiBuilder) initialize() (err error) {
	inst.builder.buf.Reset()
	// reserve space for encoding frame length
	//_, err = inst.builder.buf.Write([]byte{0, 0, 0, 0})
	return
}

func (inst *RetainedFffiBuilder) BuildRetained() *RetainedFffiHolder {
	raw := inst.builder.buf.Bytes()
	id := RetainedElementId(xxh3.Hash(raw))
	handle := unique.Make(string(raw)) // interns content; copies into a deduplicated string
	content := unsafeperf.UnsafeStringToByte(handle.Value())

	woff := inst.widgetIdOffset
	inst.putInPool()
	return &RetainedFffiHolder{
		interned:          handle,
		content:           content,
		retainedElementId: id,
		widgetIdOffset:    woff,
	}
}
func (inst *RetainedFffiBuilder) putInPool() {
	n := uint64(inst.builder.buf.Len())
	inst.builder.buf.Reset()
	if n >= builderHintParticipationFloor {
		runtime.FoldSizeHint(&builderSizeHint, n)
	}
	if uint64(inst.builder.buf.Cap()) <= runtime.PooledBufferCeiling(builderSizeHint.Load()) {
		// see https://github.com/golang/go/issues/23199
		retainedFffiBuilderPool.Put(inst.builder)
	}
	inst.builder = nil
}

// SpliceDeferredBlockMap writes the deferred block map directly into the
// builder's buffer. Called by generated Send() methods for widgets that
// consume deferred blocks (e.g. EndETable).
func (inst *RetainedFffiBuilder) SpliceDeferredBlockMap(scope *runtime.DeferredBlockScope) {
	err := scope.WriteToFixedKey(inst.builder.buf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to splice deferred block map")
	}
}

func (inst *RetainedFffiBuilder) SendIntermediate() {
	defer inst.putInPool()
	currentFffiErrorHandler(currentFffiVar.SendIntermediate(inst.builder.buf.Bytes()))
	return
}
func (inst *RetainedFffiHolder) SyncRetained() {
	currentFffiErrorHandler(currentFffiVar.SyncRetained(uint64(inst.retainedElementId), inst.content))
}
