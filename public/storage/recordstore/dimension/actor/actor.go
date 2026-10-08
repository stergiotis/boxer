// Package actor is the second dimension.Store instance (ADR-0295 §SD8, over
// ADR-0112): it interns the claimed principal, purpose and app of a call
// identity to a surrogate id and stores the descriptor once per distinct
// triple. Registered as a stamper on a payload store, it gives every row the
// store writes in-row attribution.
//
// The stamper is fail-fast (ADR-0112 SD2): a context without a call identity,
// or with an empty principal, yields an error, so a store configured with it
// refuses to commit an unattributed row — including every row written through
// the context-free Begin. The payload store's ordered flush (ADR-0112 SD5)
// keeps a descriptor durable no later than the rows referencing it, unless the
// store sets BestEffortStampFlush, which an actor-stamped store should not.
//
// Run and instance stay out of the key: they would mint one descriptor per
// run. The principal is a pseudonymous reference, never a personal value —
// the descriptor table holds it verbatim.
//
// A Recorder is single-goroutine, like the dimension.Store it wraps.
package actor

import (
	"context"
	"encoding/binary"
	"errors"
	"iter"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/dimension"
)

// ErrNoPrincipal is what the stamper yields for a context whose call identity
// is absent or names no principal. Match with errors.Is.
var ErrNoPrincipal = errors.New("no call identity with a principal on the context; an actor-stamped store does not write unattributed rows")

// Recorder interns actors through a dimension.Store.
type Recorder struct {
	dim *dimension.Store[Actor]
}

// NewRecorder wires a Recorder over an interning id generator (its
// global-uniqueness and durability strategy is the caller's, per ADR-0111) and
// the generated store's sink (NewStoreSink).
func NewRecorder(gen identifier.IdGeneratorI, sink dimension.DescriptorSink[Actor]) (inst *Recorder) {
	inst = &Recorder{dim: dimension.New(gen, sink)}
	return
}

// Reference interns ci's (principal, purpose, app) and returns the surrogate
// id it maps to. An empty principal is refused with ErrNoPrincipal.
func (inst *Recorder) Reference(ctx context.Context, ci callident.CallIdentity) (id identifier.TaggedId, err error) {
	if ci.Claims.Principal == "" {
		err = eh.Errorf("intern actor: %w", ErrNoPrincipal)
		return
	}
	a := Actor{Principal: ci.Claims.Principal, Purpose: ci.Claims.Purpose, App: ci.Origin.App}
	return inst.dim.Reference(ctx, key(a), func() Actor { return a })
}

// Stamper adapts the Recorder to recordstore.ReferenceStamper: register it on
// a payload store via <Store>StoreConfig.Stampers and write through BeginCtx
// (or DeleteCtx, Ingest<Kind>Ctx) with a call identity on the context. Do not
// register it on the actor store itself.
func (inst *Recorder) Stamper() recordstore.ReferenceStamper { return stamper{inst} }

type stamper struct{ r *Recorder }

func (s stamper) Current(ctx context.Context) iter.Seq2[identifier.TaggedId, error] {
	return func(yield func(identifier.TaggedId, error) bool) {
		ci, _ := callident.CallIdentityFrom(ctx)
		id, err := s.r.Reference(ctx, ci)
		yield(id, err)
	}
}

func (s stamper) Flush(ctx context.Context) (int, error) { return s.r.Flush(ctx) }

// Resolve returns the actor a surrogate id was minted for.
func (inst *Recorder) Resolve(ctx context.Context, id identifier.TaggedId) (Actor, bool, error) {
	return inst.dim.Resolve(ctx, id)
}

// Flush makes buffered descriptors durable.
func (inst *Recorder) Flush(ctx context.Context) (int, error) { return inst.dim.Flush(ctx) }

// key is the natural key of an actor: its three fields, each length-prefixed
// so no field's content can shift the boundary to the next.
func key(a Actor) []byte {
	buf := make([]byte, 0, 3*binary.MaxVarintLen64+len(a.Principal)+len(a.Purpose)+len(a.App))
	for _, f := range [...]string{a.Principal, a.Purpose, a.App} {
		buf = binary.AppendUvarint(buf, uint64(len(f)))
		buf = append(buf, f...)
	}
	return buf
}

// --- sink over the generated ActorStore ---

// descriptorOrder is the fixed envelope Order every descriptor carries:
// descriptors are content-addressed and immutable, one version per id.
var descriptorOrder = recordstore.SeqTs(1)

type storeSink struct {
	st    *ActorStore
	cache *ActorCache[struct{}]
}

// NewStoreSink adapts the generated ActorStore to dimension.DescriptorSink,
// with a read-through cache view for Resolve; a locally interned descriptor
// resolves before it is flushed.
func NewStoreSink(st *ActorStore) dimension.DescriptorSink[Actor] {
	return storeSink{st: st, cache: NewActorCache[struct{}](st, ActorCacheConfig{})}
}

func (inst storeSink) Emit(_ context.Context, id uint64, d Actor) error {
	d.ID = id
	return inst.st.Begin(id, descriptorOrder).AddActor(d).Commit()
}

func (inst storeSink) Resolve(ctx context.Context, id uint64) (d Actor, found bool, err error) {
	var ent *ActorEntity
	ent, found, err = inst.cache.GetFetch(ctx, id)
	if err != nil || !found {
		return
	}
	d = ent.Actor.Val
	found = ent.Actor.Has
	return
}

func (inst storeSink) Flush(ctx context.Context) (int, error) { return inst.st.Flush(ctx) }
