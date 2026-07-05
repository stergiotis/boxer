// Package seq provides persistent, per-tag monotonic identifier.IdGeneratorI
// implementations: each hands out a dense, increasing stream of ids for one tag
// and ignores the natural key. The Badger backend leases a bandwidth-sized block
// of ids per disk write via badger.DB.GetSequence. For natural-key deduplication
// (get-or-assign) use the sibling internalized package instead.
package seq

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/dgraph-io/badger/v4"
	badger2 "github.com/stergiotis/boxer/public/db/badger"
	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var _ identifier.IdGeneratorFactoryI = (*BadgerIdSequenceGenerator)(nil)
var _ identifier.IdGeneratorI = (*BadgerIdSequence)(nil)

// BadgerIdSequenceGenerator is a Badger-backed factory for per-tag sequential
// id generators; one embedded store may host many tags, each with at most one
// generator (identgen.ErrTagInUse otherwise — a second generator would lease
// disjoint id blocks and interleave them, breaking the dense increasing
// stream this package promises).
type BadgerIdSequenceGenerator struct {
	kv    *badger.DB
	mu    sync.Mutex
	inUse map[identifier.TagValue]struct{}
}

// BadgerIdSequence hands out monotonically increasing ids for one tag, leased
// from a badger.Sequence. It is safe for concurrent use.
type BadgerIdSequence struct {
	gen   *BadgerIdSequenceGenerator
	seq   *badger.Sequence
	tag   identifier.IdTag
	maxId uint64
}

func (inst *BadgerIdSequence) GetId(naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	var untagged identifier.UntaggedId
	untagged, fresh, err = inst.GetUntaggedId(naturalKey)
	if err != nil {
		return
	}
	id = inst.tag.ComposeId(untagged)
	return
}

// GetUntaggedId hands out the next id in the tag's stream. The natural key is
// ignored by contract (see identifier.IdGeneratorI); it used to be warned
// about per call, which spammed the log of any caller using the seam
// generically.
func (inst *BadgerIdSequence) GetUntaggedId(naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	fresh = true
	var raw uint64
	raw, err = inst.seq.Next()
	if err != nil {
		return
	}
	u := raw + 1 // body 0 is reserved as invalid/NULL
	if u > inst.maxId {
		err = eb.Build().Uint64("tagValue", uint64(inst.tag.GetValue())).Uint64("untaggedId", u).Errorf("cannot mint a fresh id: %w", identgen.ErrIdSpaceExhausted)
		return
	}
	untagged = identifier.UntaggedId(u)
	return
}

func (inst *BadgerIdSequence) Release() (err error) {
	err = inst.seq.Release()
	return
}

func (inst *BadgerIdSequence) GetTag() (tag identifier.IdTag) {
	tag = inst.tag
	return
}

// Create returns the sequential generator for tagValue. At most one generator
// per tag and store may exist (identgen.ErrTagInUse); the slot is held until
// the factory closes — Release keeps its generator usable and therefore does
// not free the slot.
func (inst *BadgerIdSequenceGenerator) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	if !tagValue.IsValid() {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("invalid tag value (zero is reserved)")
		return
	}
	if generationBandwidth == 0 {
		err = eh.Errorf("generation bandwidth is zero")
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.inUse[tagValue]; dup {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("cannot create a second sequential generator: %w", identgen.ErrTagInUse)
		return
	}
	k := make([]byte, 9)
	binary.LittleEndian.PutUint64(k, uint64(tagValue))
	var seq *badger.Sequence
	seq, err = inst.kv.GetSequence(k, generationBandwidth)
	if err != nil {
		err = eh.Errorf("unable to create sequence: %w", err)
		return
	}
	inst.inUse[tagValue] = struct{}{}
	tag := tagValue.GetTag()
	gen = &BadgerIdSequence{
		gen:   inst,
		seq:   seq,
		tag:   tag,
		maxId: uint64(tag.GetMaxPossibleIdIncl()),
	}
	return
}

func NewBadgerIdSequenceGenerator(storePath string) (inst *BadgerIdSequenceGenerator, err error) {
	var kv *badger.DB
	opts := badger.DefaultOptions(storePath).WithLogger(&badger2.ZerologLoggerAdapter{})
	kv, err = badger.Open(opts)
	if err != nil {
		err = eb.Build().Str("storePath", storePath).Errorf("unable to open key value store database: %w", err)
		return
	}
	inst = &BadgerIdSequenceGenerator{
		kv:    kv,
		inUse: make(map[identifier.TagValue]struct{}),
	}
	return
}

// Compact runs one value-log GC pass to reclaim space. Generators no longer
// compact implicitly, so callers should invoke this periodically for long-lived
// stores.
func (inst *BadgerIdSequenceGenerator) Compact() (err error) {
	err = inst.kv.RunValueLogGC(0.5)
	if errors.Is(err, badger.ErrNoRewrite) {
		err = nil
	}
	return
}

func (inst *BadgerIdSequenceGenerator) Close() (err error) {
	err = inst.kv.Close()
	return
}
