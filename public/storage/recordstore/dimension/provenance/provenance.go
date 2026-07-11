// Package provenance is the first dimension.Store instance (ADR-0112 SD6): it
// interns a writer's (host, Go call-stack) to a surrogate id and stores the
// descriptor — hostname and symbolicated frames — once per distinct stack. The
// compact id is what slice S2 will stamp onto payload attributes; the heavy
// descriptor lives here, one row per stack.
//
// Capture is cheap on the hot path — runtime.Callers only; symbolication runs
// once, on first sight of a stack, behind the dimension.Store's fresh gate. A
// Recorder is single-goroutine, like the Store it wraps.
package provenance

import (
	"context"
	"encoding/binary"
	"fmt"
	"iter"
	"os"
	"runtime"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/dimension"
)

// maxDepth bounds the captured call stack — the innermost 64 frames. Two call
// paths identical in that window but diverging below it intern to one id and
// share a descriptor; deep-recursion provenance is attributed to the nearest
// 64 frames.
const maxDepth = 64

// Recorder captures provenance and interns it through a dimension.Store.
type Recorder struct {
	dim  *dimension.Store[Provenance]
	host string
	skip int
}

// NewRecorder wires a Recorder over an interning id generator (its
// global-uniqueness / durability strategy is the caller's choice, per ADR-0111)
// and the generated store's sink (NewStoreSink). The hostname is read once.
func NewRecorder(gen identifier.IdGeneratorI, sink dimension.DescriptorSink[Provenance]) (inst *Recorder, err error) {
	host, err := os.Hostname()
	if err != nil {
		err = eh.Errorf("read hostname: %w", err)
		return
	}
	// Skip runtime.Callers and Recorder.Reference, so the first captured frame
	// is the code that called Reference. (Under the S2 builder seam the stamper
	// is invoked from generated code, so the skip there is larger — an S2
	// concern, not this standalone recorder's.)
	inst = &Recorder{dim: dimension.New(gen, sink), host: host, skip: 2}
	return
}

// Reference captures the caller's stack, interns (host, stack) and returns the
// surrogate id it maps to. Symbolication runs only on the first sight of a
// stack, inside the fresh-gated describe.
func (inst *Recorder) Reference(ctx context.Context) (id identifier.TaggedId, err error) {
	return inst.reference(ctx, inst.skip)
}

// reference captures the stack skip frames above runtime.Callers and interns
// it. Reference uses inst.skip (direct call); the Stamper adapter passes a
// deeper skip for the store call path.
func (inst *Recorder) reference(ctx context.Context, skip int) (id identifier.TaggedId, err error) {
	var pcbuf [maxDepth]uintptr
	n := runtime.Callers(skip, pcbuf[:])
	pcs := make([]uintptr, n)
	copy(pcs, pcbuf[:n])
	return inst.dim.Reference(ctx, inst.key(pcs), func() Provenance {
		return Provenance{Host: inst.host, Stack: symbolicate(pcs)}
	})
}

// stamperCaptureSkip trims runtime.Callers, Recorder.reference and the Current
// seq closure, so the first captured frame is the store's applyStampers and the
// user's Begin call site follows — enough to distinguish write sites in the key
// and to attribute the row. The two store frames it leaves (applyStampers,
// Begin) are honest context, not noise to hide.
const stamperCaptureSkip = 3

// Stamper adapts the Recorder to recordstore.ReferenceStamper, so it can be
// registered on a payload store via <Store>StoreConfig.Stampers and consulted
// on every Begin. Do not register a provenance store's own recorder on that
// same store — interning a fact would recurse.
func (inst *Recorder) Stamper() recordstore.ReferenceStamper { return stamper{inst} }

type stamper struct{ r *Recorder }

func (s stamper) Current(ctx context.Context) iter.Seq2[identifier.TaggedId, error] {
	return func(yield func(identifier.TaggedId, error) bool) {
		id, err := s.r.reference(ctx, stamperCaptureSkip)
		yield(id, err)
	}
}

// Flush makes the interned provenance facts durable — a payload store calls it
// before its own insert (ADR-0112 SD5).
func (s stamper) Flush(ctx context.Context) (int, error) { return s.r.Flush(ctx) }

// Resolve returns the host and frames a surrogate id was minted for.
func (inst *Recorder) Resolve(ctx context.Context, id identifier.TaggedId) (Provenance, bool, error) {
	return inst.dim.Resolve(ctx, id)
}

// Flush makes buffered descriptors durable.
func (inst *Recorder) Flush(ctx context.Context) (int, error) { return inst.dim.Flush(ctx) }

// key builds the natural key from host + raw program counters. It must be
// stable for one logical stack across the runs whose ids share a durable
// generator.
//
// Caveat (S1): raw pcs are stable across restarts only for a fixed-text
// (non-PIE) build of one binary; a module-relative or symbol-derived key is the
// ASLR-robust / cross-build refinement (deferred, ADR-0112). A production build
// would encode via leeway/stopa/naturalkey; this NUL-separated concat (host
// cannot contain NUL; pcs are fixed-width) keeps the standalone slice
// dependency-light.
func (inst *Recorder) key(pcs []uintptr) []byte {
	buf := make([]byte, 0, len(inst.host)+1+len(pcs)*8)
	buf = append(buf, inst.host...)
	buf = append(buf, 0)
	var b [8]byte
	for _, pc := range pcs {
		binary.LittleEndian.PutUint64(b[:], uint64(pc))
		buf = append(buf, b[:]...)
	}
	return buf
}

func symbolicate(pcs []uintptr) (frames []string) {
	if len(pcs) == 0 {
		return
	}
	cf := runtime.CallersFrames(pcs)
	for {
		f, more := cf.Next()
		frames = append(frames, fmt.Sprintf("%s (%s:%d)", f.Function, f.File, f.Line))
		if !more {
			break
		}
	}
	return
}

// --- sink over the generated ProvenanceStore ---

// descriptorOrder is the fixed envelope Order every descriptor carries:
// descriptors are content-addressed and immutable — one logical version per
// id — so a single non-zero Order suffices for Latest.
var descriptorOrder = recordstore.SeqTs(1)

type storeSink struct {
	st *ProvenanceStore
	// cache is the store's attached read-through view (ADR-0112 SD1): Resolve
	// is a cached point lookup — the hot path for readers, which resolve few
	// distinct ids across many stamped attributes. Write-through means a
	// locally interned descriptor resolves immediately, before Flush; other
	// processes see it once durable.
	cache *ProvenanceCache[struct{}]
}

// NewStoreSink adapts the generated ProvenanceStore to dimension.DescriptorSink,
// attaching a read-through cache view for Resolve.
func NewStoreSink(st *ProvenanceStore) dimension.DescriptorSink[Provenance] {
	return storeSink{st: st, cache: NewProvenanceCache[struct{}](st, ProvenanceCacheConfig{})}
}

func (inst storeSink) Emit(_ context.Context, id uint64, d Provenance) error {
	// Begin(id, ts): the descriptor schema has no pass-through envelope and no
	// lifecycle. The id is the key; it is mirrored into d.ID so the cache's
	// write-through twin matches the decoded read (which fills ID from the
	// plain column).
	d.ID = id
	return inst.st.Begin(id, descriptorOrder).AddProvenance(d).Commit()
}

func (inst storeSink) Resolve(ctx context.Context, id uint64) (d Provenance, found bool, err error) {
	var ent *ProvenanceEntity
	ent, found, err = inst.cache.GetFetch(ctx, id)
	if err != nil || !found {
		return
	}
	d = ent.Provenance.Val
	found = ent.Provenance.Has
	return
}

func (inst storeSink) Flush(ctx context.Context) (int, error) { return inst.st.Flush(ctx) }
