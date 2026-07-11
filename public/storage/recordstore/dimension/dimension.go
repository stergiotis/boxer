// Package dimension is the ADR-0112 slice-S1 runtime: a DimensionStore interns
// a natural key to a surrogate identifier.TaggedId via an injected
// identifier.IdGeneratorI, emits a descriptor fact exactly once (on the
// generator's first-sight "fresh" signal), and resolves an id back to its
// descriptor. The heavy descriptor is stored once; only the compact id is meant
// to ride elsewhere.
//
// Stamping the id onto payload attributes as an additive membership is slice S2
// (the leeway DML seam); this package knows nothing about memberships or
// provenance — provenance is one instance, in the sibling provenance package.
//
// Like everything it composes (the id generator, the descriptor store and its
// cache), a Store is single-goroutine; use one instance per goroutine.
package dimension

import (
	"context"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// DescriptorSink is the store-shaped seam a Store writes descriptors through and
// resolves them from — a generated recordstore store adapts to it. The id is the
// full TaggedId (as uint64): globally unique under both ADR-0111 id strategies
// (per-host tag, shared-tag block-lease), so it is a sound primary key, and the
// tag lets a reader tell dimension ids apart from other ids in a shared lane.
type DescriptorSink[D any] interface {
	// Emit buffers one descriptor row keyed by id. Append-only: an idempotent
	// re-emit of the same id is harmless (ADR-0100 SD3).
	Emit(ctx context.Context, id uint64, d D) error
	// Resolve returns the descriptor for id (newest row), or found=false.
	Resolve(ctx context.Context, id uint64) (d D, found bool, err error)
	// Flush makes buffered descriptors durable.
	Flush(ctx context.Context) (n int, err error)
}

// Store is the DimensionStore of ADR-0112 SD1: a thin wrapper over an interning
// id generator and a descriptor sink. D is the descriptor value type.
type Store[D any] struct {
	gen  identifier.IdGeneratorI
	sink DescriptorSink[D]
	// emitting guards Reference against re-entry: an Emit that finds its way
	// back into the same Store's Reference — a stamper writing through its
	// own descriptor store — would otherwise recurse, minting garbage ids
	// until the bounded stack-capture window makes the keys repeat.
	emitting bool
	// retryEmit holds ids whose descriptor Emit failed after the key was
	// already interned. The generator never reports such a key fresh again,
	// so without the retry one transient Emit failure would orphan the id
	// for the generator's lifetime — every later payload row referencing a
	// descriptor that never gets written. Reference retries the emission on
	// the next sight of the key. (The cross-restart analogue — a durable
	// generator whose lease outlives an unflushed descriptor — is ADR-0112
	// SD3's recorded ADR-0111 integration concern.)
	retryEmit map[identifier.TaggedId]struct{}
}

// New wires a Store. gen must be an INTERNALIZING generator (one that dedupes by
// natural key, so fresh marks first sight); a sequential generator ignores the
// key and always mints, which would re-emit on every call. The generator's
// global-uniqueness and durability strategy is ADR-0111's concern, not this
// package's — Store only relies on the (id, fresh) contract.
func New[D any](gen identifier.IdGeneratorI, sink DescriptorSink[D]) *Store[D] {
	return &Store[D]{gen: gen, sink: sink}
}

// Reference interns key and returns the surrogate id it maps to. describe is
// invoked only when the descriptor must be emitted — on the first sight of
// key, or on a retry after a failed emission — so the caller can defer an
// expensive descriptor construction (e.g. stack symbolication) behind it: a
// key already seen and emitted costs one generator lookup and nothing more.
//
// A re-entrant call — Emit ending up back in this Store's Reference — errors
// instead of recursing.
func (inst *Store[D]) Reference(ctx context.Context, key []byte, describe func() D) (id identifier.TaggedId, err error) {
	if inst.emitting {
		err = eh.Errorf("re-entrant dimension Reference: a stamper must not write through its own descriptor store")
		return
	}
	id, fresh, err := inst.gen.GetId(ctx, key)
	if err != nil {
		err = eh.Errorf("intern dimension key: %w", err)
		return
	}
	if !fresh {
		if _, retry := inst.retryEmit[id]; !retry {
			return
		}
	}
	inst.emitting = true
	defer func() { inst.emitting = false }()
	if err = inst.sink.Emit(ctx, uint64(id), describe()); err != nil {
		err = eh.Errorf("emit dimension descriptor %d: %w", uint64(id), err)
		if inst.retryEmit == nil {
			inst.retryEmit = make(map[identifier.TaggedId]struct{}, 1)
		}
		inst.retryEmit[id] = struct{}{}
		return
	}
	delete(inst.retryEmit, id)
	return
}

// Resolve returns the descriptor an id was minted for. Visibility is the
// sink's contract: the provenance sink's write-through cache resolves a
// locally emitted descriptor immediately, while another process sees it only
// once flushed.
func (inst *Store[D]) Resolve(ctx context.Context, id identifier.TaggedId) (d D, found bool, err error) {
	return inst.sink.Resolve(ctx, uint64(id))
}

// Flush makes buffered descriptors durable. In S2 the payload store drives this
// before its own insert (ordered flush, ADR-0112 SD5); standalone, the caller
// flushes.
func (inst *Store[D]) Flush(ctx context.Context) (n int, err error) {
	return inst.sink.Flush(ctx)
}
