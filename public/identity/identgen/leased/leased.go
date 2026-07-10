// Package leased generates identifier.IdGeneratorI ids over a technology-neutral
// identgen.AllocatorI: the id source is a swappable block allocator rather than a
// hard-wired embedded store. A local allocator advances a persisted per-tag
// counter; a remote one leases blocks from a network authority, letting many
// independent processes draw from one id space with no shared local storage. The
// allocator is touched once per block, not once per id.
//
// Two generators sit on the shared block cursor: Sequence hands out a dense,
// key-agnostic monotonic stream (the neutral analogue of the Badger-backed seq
// package), and Internalizer (internalizer.go) adds a local get-or-assign map.
// Unlike a store-native factory, this one imposes no one-generator-per-tag
// restriction: the allocator serialises reservations, so any number of
// generators may share a tag and simply draw disjoint blocks.
package leased

import (
	"context"
	"sync"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var _ identifier.IdGeneratorI = (*Sequence)(nil)
var _ identifier.IdGeneratorFactoryI = (*SequenceFactory)(nil)

// cursor hands out body (untagged) values under one tag, leasing a fresh block
// from the allocator when the current one is spent and clamping to the tag's
// body capacity. It is not safe for concurrent use; the owning generator
// serialises calls to next.
type cursor struct {
	alloc     identgen.AllocatorI
	tagValue  identifier.TagValue
	tag       identifier.IdTag
	bandwidth uint64
	maxId     identifier.UntaggedId // largest body value the tag can hold (inclusive)
	cur       identifier.UntaggedId // next body value to hand out
	hi        identifier.UntaggedId // exclusive upper bound of the current lease
}

func newCursor(alloc identgen.AllocatorI, tagValue identifier.TagValue, generationBandwidth uint64) (inst *cursor, err error) {
	if alloc == nil {
		err = eh.Errorf("allocator is nil")
		return
	}
	if !tagValue.IsValid() {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("invalid tag value (zero is reserved)")
		return
	}
	if generationBandwidth == 0 {
		err = eh.Errorf("generation bandwidth is zero")
		return
	}
	tag := tagValue.GetTag()
	inst = &cursor{
		alloc:     alloc,
		tagValue:  tagValue,
		tag:       tag,
		bandwidth: generationBandwidth,
		maxId:     tag.GetMaxPossibleIdIncl(),
		// cur == hi == 0: the first next leases a block.
	}
	return
}

// next hands out the next body value, leasing a fresh block when the current one
// is spent. It returns identgen.ErrIdSpaceExhausted once the tag's body range is
// used up.
func (inst *cursor) next(ctx context.Context) (untagged identifier.UntaggedId, err error) {
	if inst.cur >= inst.hi {
		if err = inst.lease(ctx); err != nil {
			return
		}
	}
	untagged = inst.cur
	inst.cur++
	return
}

// lease reserves the next block from the allocator, clamped to the tag's body
// ceiling. Bodies above maxId would overflow into the tag region, so a block
// that starts above the ceiling means the tag's id space is exhausted.
func (inst *cursor) lease(ctx context.Context) (err error) {
	if inst.cur > inst.maxId {
		err = eb.Build().Uint64("tagValue", uint64(inst.tagValue)).Uint64("maxIdIncl", uint64(inst.maxId)).Errorf("cannot mint a fresh id: %w", identgen.ErrIdSpaceExhausted)
		return
	}
	var lo, hi identifier.UntaggedId
	lo, hi, err = inst.alloc.AllocateBlock(ctx, inst.tagValue, 1, inst.bandwidth)
	if err != nil {
		return
	}
	if hi > inst.maxId+1 {
		hi = inst.maxId + 1
	}
	if lo > inst.maxId || lo >= hi {
		err = eb.Build().Uint64("tagValue", uint64(inst.tagValue)).Uint64("maxIdIncl", uint64(inst.maxId)).Errorf("cannot mint a fresh id: %w", identgen.ErrIdSpaceExhausted)
		return
	}
	inst.cur = lo
	inst.hi = hi
	return
}

// Sequence hands out a dense, monotonically increasing, key-agnostic id stream
// for one tag, drawing blocks from an identgen.AllocatorI. The natural key is
// ignored by contract (see identifier.IdGeneratorI). It is safe for concurrent
// use; a block refill holds the lock, so callers briefly serialise across a
// (possibly remote) allocation. The ctx passed to GetId/GetUntaggedId bounds
// that refill and may cancel it.
type Sequence struct {
	mu     sync.Mutex
	cursor *cursor
}

// NewSequence builds a leased sequential generator for tagValue over alloc.
// generationBandwidth is the block size leased per allocator call.
func NewSequence(alloc identgen.AllocatorI, tagValue identifier.TagValue, generationBandwidth uint64) (inst *Sequence, err error) {
	var c *cursor
	c, err = newCursor(alloc, tagValue, generationBandwidth)
	if err != nil {
		return
	}
	inst = &Sequence{cursor: c}
	return
}

func (inst *Sequence) GetUntaggedId(ctx context.Context, naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	untagged, err = inst.cursor.next(ctx)
	if err != nil {
		return
	}
	fresh = true
	return
}

func (inst *Sequence) GetId(ctx context.Context, naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	var untagged identifier.UntaggedId
	untagged, fresh, err = inst.GetUntaggedId(ctx, naturalKey)
	if err != nil {
		return
	}
	id = inst.cursor.tag.ComposeId(untagged)
	return
}

func (inst *Sequence) GetTag() (tag identifier.IdTag) {
	return inst.cursor.tag
}

// Release satisfies identifier.IdGeneratorI. The unused tail of the current
// lease is dropped (a gap, never a repeat); a returning allocator could reclaim
// it. The generator stays usable — a later GetId leases a fresh block (the
// documented post-Release performance penalty).
func (inst *Sequence) Release() (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cursor.cur = inst.cursor.hi // force a fresh lease on next use
	return
}

// SequenceFactory builds leased sequential generators over a shared allocator.
// It imposes no one-generator-per-tag restriction (the allocator serialises
// reservations); Close closes the underlying allocator.
type SequenceFactory struct {
	alloc identgen.AllocatorI
}

// NewSequenceFactory returns a factory minting Sequence generators over alloc.
func NewSequenceFactory(alloc identgen.AllocatorI) (inst *SequenceFactory) {
	return &SequenceFactory{alloc: alloc}
}

func (inst *SequenceFactory) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	var s *Sequence
	s, err = NewSequence(inst.alloc, tagValue, generationBandwidth)
	if err != nil {
		return
	}
	gen = s
	return
}

func (inst *SequenceFactory) Close() (err error) {
	return inst.alloc.Close()
}
