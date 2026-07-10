package leased

import (
	"context"
	"sync"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

var _ identifier.IdGeneratorI = (*Internalizer)(nil)
var _ identifier.IdGeneratorFactoryI = (*InternalizerFactory)(nil)

// Internalizer is a get-or-assign generator that deduplicates natural keys in a
// local map while drawing fresh ids from a (possibly global) identgen.AllocatorI.
//
// Dedup scope is this one instance: a key seen twice here resolves to the same
// id. Two Internalizer instances over the same allocator do NOT share their
// maps, so the same key resolves to two different — but globally unique — ids.
// That is the deliberate coordination-free trade-off: global uniqueness without
// global coordination. True cross-instance dedup would require the authority to
// own the key->id mapping (a heavier, coordinating service), which this type
// intentionally avoids so the allocator stays a dumb, high-throughput range
// server. It is safe for concurrent use. The ctx passed to GetId/GetUntaggedId
// bounds a block refill (the only place it can block) and may cancel it.
type Internalizer struct {
	mu      sync.Mutex
	cursor  *cursor
	forward map[string]identifier.UntaggedId
	reverse map[identifier.UntaggedId]string
}

// NewInternalizer builds a leased internalizer for tagValue over alloc.
// generationBandwidth is the block size leased per allocator call.
func NewInternalizer(alloc identgen.AllocatorI, tagValue identifier.TagValue, generationBandwidth uint64) (inst *Internalizer, err error) {
	var c *cursor
	c, err = newCursor(alloc, tagValue, generationBandwidth)
	if err != nil {
		return
	}
	inst = &Internalizer{
		cursor:  c,
		forward: make(map[string]identifier.UntaggedId),
		reverse: make(map[identifier.UntaggedId]string),
	}
	return
}

func (inst *Internalizer) GetUntaggedId(ctx context.Context, naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	if len(naturalKey) == 0 {
		err = identgen.ErrEmptyNaturalKey
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if u, has := inst.forward[string(naturalKey)]; has {
		untagged = u
		return
	}
	untagged, err = inst.cursor.next(ctx)
	if err != nil {
		return
	}
	s := string(naturalKey)
	inst.forward[s] = untagged
	inst.reverse[untagged] = s
	fresh = true
	return
}

func (inst *Internalizer) GetId(ctx context.Context, naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	var untagged identifier.UntaggedId
	untagged, fresh, err = inst.GetUntaggedId(ctx, naturalKey)
	if err != nil {
		return
	}
	id = inst.cursor.tag.ComposeId(untagged)
	return
}

func (inst *Internalizer) GetTag() (tag identifier.IdTag) {
	return inst.cursor.tag
}

// Release satisfies identifier.IdGeneratorI. The local key map is retained (the
// internalizer stays usable); only the unused tail of the current lease is
// dropped, so a later fresh key leases a new block.
func (inst *Internalizer) Release() (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cursor.cur = inst.cursor.hi
	return
}

// Len reports the number of distinct keys internalized so far.
func (inst *Internalizer) Len() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.forward)
}

// Resolve maps a tagged id minted by this internalizer back to its natural key.
// found is false when the id carries a different tag or was never assigned on
// this instance.
func (inst *Internalizer) Resolve(id identifier.TaggedId) (naturalKey string, found bool) {
	tag, untagged := id.Split()
	if tag != inst.cursor.tag {
		return
	}
	return inst.ResolveUntagged(untagged)
}

// ResolveUntagged maps a body (untagged) id back to its natural key on this
// instance.
func (inst *Internalizer) ResolveUntagged(untagged identifier.UntaggedId) (naturalKey string, found bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	naturalKey, found = inst.reverse[untagged]
	return
}

// InternalizerFactory builds leased internalizers over a shared allocator. As
// with SequenceFactory there is no one-generator-per-tag restriction, but note
// each generator dedups independently: share one instance per tag within a
// process to get process-wide dedup.
type InternalizerFactory struct {
	alloc identgen.AllocatorI
}

// NewInternalizerFactory returns a factory minting Internalizer generators over
// alloc.
func NewInternalizerFactory(alloc identgen.AllocatorI) (inst *InternalizerFactory) {
	return &InternalizerFactory{alloc: alloc}
}

func (inst *InternalizerFactory) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	var m *Internalizer
	m, err = NewInternalizer(inst.alloc, tagValue, generationBandwidth)
	if err != nil {
		return
	}
	gen = m
	return
}

func (inst *InternalizerFactory) Close() (err error) {
	return inst.alloc.Close()
}
