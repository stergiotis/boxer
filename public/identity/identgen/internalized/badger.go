// Package internalized is the Badger-backed get-or-assign identifier.IdGeneratorI:
// it maps a natural key to a stable surrogate id, persisting the mapping in an
// embedded store. The dependency-free in-memory backend lives in the sibling mem
// package; for key-agnostic monotonic ids use the seq package.
package internalized

import (
	"encoding/binary"
	"errors"
	"slices"
	"sync"

	"github.com/dgraph-io/badger/v4"
	badger2 "github.com/stergiotis/boxer/public/db/badger"
	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var _ identifier.IdGeneratorFactoryI = (*BadgerIdInternalizedGenerator)(nil)
var _ identifier.IdGeneratorI = (*BadgerIdInternalizer)(nil)
var _ identgen.BatchInternalizerI = (*BadgerIdInternalizer)(nil)

// BadgerIdInternalizedGenerator is a Badger-backed factory for per-tag
// internalizing id generators; a single embedded store may host many tags,
// each with at most one generator (identgen.ErrTagInUse otherwise).
type BadgerIdInternalizedGenerator struct {
	kv    *badger.DB
	mu    sync.Mutex
	inUse map[identifier.TagValue]struct{}
}

// BadgerIdInternalizer maps a natural key to a stable id under one tag,
// persisting the mapping in Badger and minting fresh ids from a leased sequence.
// It is safe for concurrent use.
type BadgerIdInternalizer struct {
	gen    *BadgerIdInternalizedGenerator
	seq    *badger.Sequence
	tag    identifier.IdTag
	prefix []byte // tag-scoped mapping-key prefix: tagValue, little-endian, 8 bytes
	maxId  uint64
	lock   sync.Mutex
}

// mappingKey returns the tag-scoped Badger key for naturalKey: prefix ++
// naturalKey. Tag-scoping keeps distinct tags sharing one store from colliding;
// the sequence counter lives at the 8-byte prefix alone, which is length-disjoint
// from every (non-empty) mapping key.
func (inst *BadgerIdInternalizer) mappingKey(naturalKey []byte) (key []byte) {
	key = make([]byte, 0, len(inst.prefix)+len(naturalKey))
	key = append(key, inst.prefix...)
	key = append(key, naturalKey...)
	return
}

func (inst *BadgerIdInternalizer) GetUntaggedId(naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	if len(naturalKey) == 0 {
		err = identgen.ErrEmptyNaturalKey
		return
	}
	inst.lock.Lock()
	defer inst.lock.Unlock()

	key := inst.mappingKey(naturalKey)

	var u uint64
	err = inst.gen.kv.View(func(txn *badger.Txn) (e error) {
		var item *badger.Item
		item, e = txn.Get(key)
		if e != nil {
			return
		}
		return item.Value(func(val []byte) error {
			u = binary.LittleEndian.Uint64(val)
			return nil
		})
	})
	if err == nil {
		untagged = identifier.UntaggedId(u)
		return
	}
	if !errors.Is(err, badger.ErrKeyNotFound) {
		err = eh.Errorf("unable to read id: %w", err)
		return
	}

	// First sight of this key: mint a fresh id and persist the mapping. The
	// View-then-Update pair is atomic under inst.lock, and Badger permits only a
	// single process per store, so no other writer can interleave.
	var raw uint64
	raw, err = inst.seq.Next()
	if err != nil {
		err = eh.Errorf("unable to obtain next sequence value: %w", err)
		return
	}
	u = raw + 1 // body 0 is reserved as invalid/NULL
	if u > inst.maxId {
		err = eb.Build().Uint64("tagValue", uint64(inst.tag.GetValue())).Uint64("untaggedId", u).Errorf("cannot mint a fresh id: %w", identgen.ErrIdSpaceExhausted)
		return
	}
	var vbuf [8]byte
	binary.LittleEndian.PutUint64(vbuf[:], u)
	err = inst.gen.kv.Update(func(txn *badger.Txn) error {
		return txn.Set(key, vbuf[:])
	})
	if err != nil {
		err = eh.Errorf("unable to persist natural-key to id mapping: %w", err)
		return
	}
	fresh = true
	untagged = identifier.UntaggedId(u)
	return
}

func (inst *BadgerIdInternalizer) GetId(naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	var untagged identifier.UntaggedId
	untagged, fresh, err = inst.GetUntaggedId(naturalKey)
	if err != nil {
		return
	}
	id = inst.tag.ComposeId(untagged)
	return
}

func (inst *BadgerIdInternalizer) Release() (err error) {
	err = inst.seq.Release()
	return
}

func (inst *BadgerIdInternalizer) GetTag() (tag identifier.IdTag) {
	tag = inst.tag
	return
}

// AppendIds resolves a whole column of natural keys under this generator's tag
// in one read transaction plus as few write transactions as fit Badger's
// transaction-size limit (usually one), amortising the per-call transaction
// overhead. See identgen.BatchInternalizerI for the chunked-commit semantics.
func (inst *BadgerIdInternalizer) AppendIds(dst []identifier.TaggedId, keys identgen.KeysColumn, fresh []bool) (ids []identifier.TaggedId, freshOut []bool, err error) {
	n := keys.Len()
	for i := range n {
		if len(keys.At(i)) == 0 {
			err = identgen.ErrEmptyNaturalKey
			return dst, fresh, err
		}
	}

	inst.lock.Lock()
	defer inst.lock.Unlock()

	// Build the tag-scoped mapping keys once and reuse them across both txns.
	// Badger retains the key/value slices passed to Set until commit, so each must
	// be distinct — mappingKey and the per-entry value windows below guarantee that.
	mks := make([][]byte, n)
	for i := range n {
		mks[i] = inst.mappingKey(keys.At(i))
	}
	bodies := make([]uint64, n)
	freshMark := make([]bool, n)

	// Read phase: resolve existing mappings in one transaction.
	err = inst.gen.kv.View(func(txn *badger.Txn) (e error) {
		for i := range n {
			item, ge := txn.Get(mks[i])
			if ge == nil {
				e = item.Value(func(val []byte) error {
					bodies[i] = binary.LittleEndian.Uint64(val)
					return nil
				})
				if e != nil {
					return
				}
			} else if errors.Is(ge, badger.ErrKeyNotFound) {
				freshMark[i] = true
			} else {
				e = ge
				return
			}
		}
		return
	})
	if err != nil {
		err = eh.Errorf("unable to read id batch: %w", err)
		return dst, fresh, err
	}

	// Mint phase: assign fresh ids for the misses, deduplicating keys that repeat
	// within this batch (the first occurrence mints; later ones reuse it, matching
	// single-key GetId). A leased badger.Sequence must not be advanced inside a
	// transaction, so this runs between the read and write phases.
	//
	// A batch that overruns the tag's remaining space fails, but the mappings
	// minted before the overrun are still persisted below: consumed sequence
	// values cannot be returned, so dropping them would burn the space with
	// nothing to show, while persisting keeps retries idempotent (the minted
	// keys resolve as existing). The in-memory backend can count the space up
	// front instead and assigns nothing on such a batch.
	minted := make(map[string]uint64)
	vals := make([]byte, 8*n)
	var exhausted error
	for i := range n {
		if !freshMark[i] {
			continue
		}
		if b, ok := minted[string(keys.At(i))]; ok {
			bodies[i] = b
			freshMark[i] = false // repeat of a key already minted in this batch
			continue
		}
		if exhausted != nil {
			freshMark[i] = false // not minted; must not reach the write phase
			continue
		}
		var raw uint64
		raw, err = inst.seq.Next()
		if err != nil {
			err = eh.Errorf("unable to obtain next sequence value: %w", err)
			return dst, fresh, err
		}
		u := raw + 1 // body 0 is reserved as invalid/NULL
		if u > inst.maxId {
			exhausted = eb.Build().Uint64("tagValue", uint64(inst.tag.GetValue())).Uint64("untaggedId", u).Errorf("cannot mint a fresh id: %w", identgen.ErrIdSpaceExhausted)
			freshMark[i] = false
			continue
		}
		bodies[i] = u
		minted[string(keys.At(i))] = u
		binary.LittleEndian.PutUint64(vals[i*8:i*8+8], u)
	}

	// Write phase: persist all fresh mappings, rolling over to a further
	// transaction whenever one fills up (ErrTxnTooBig; a single Update capped
	// batches at roughly a hundred thousand fresh keys). Splitting is safe
	// because interning is idempotent get-or-assign: should a later chunk
	// fail, the keys already committed simply resolve as existing on retry.
	txn := inst.gen.kv.NewTransaction(true)
	defer func() { txn.Discard() }()
	for i := range n {
		if !freshMark[i] {
			continue
		}
		e := txn.Set(mks[i], vals[i*8:i*8+8])
		if errors.Is(e, badger.ErrTxnTooBig) {
			e = txn.Commit()
			if e != nil {
				err = eh.Errorf("unable to persist id batch chunk: %w", e)
				return dst, fresh, err
			}
			txn = inst.gen.kv.NewTransaction(true)
			e = txn.Set(mks[i], vals[i*8:i*8+8])
		}
		if e != nil {
			err = eh.Errorf("unable to persist id batch: %w", e)
			return dst, fresh, err
		}
	}
	err = txn.Commit()
	if err != nil {
		err = eh.Errorf("unable to persist id batch: %w", err)
		return dst, fresh, err
	}
	if exhausted != nil {
		return dst, fresh, exhausted
	}

	// Assemble the output columns.
	ids = slices.Grow(dst, n)
	if fresh != nil {
		freshOut = slices.Grow(fresh, n)
	}
	for i := range n {
		ids = append(ids, inst.tag.ComposeId(identifier.UntaggedId(bodies[i])))
		if freshOut != nil {
			freshOut = append(freshOut, freshMark[i])
		}
	}
	return
}

// Create returns the internalizing generator for tagValue. At most one
// generator per tag and store may exist: a second Create for the same tag
// fails with identgen.ErrTagInUse (the per-generator lock cannot span two
// instances, so a duplicate could mint two ids for one key). The slot is held
// until the factory closes; Release keeps its generator usable and therefore
// does not free the slot.
func (inst *BadgerIdInternalizedGenerator) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
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
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("cannot create a second internalizing generator: %w", identgen.ErrTagInUse)
		return
	}
	prefix := make([]byte, 8)
	binary.LittleEndian.PutUint64(prefix, uint64(tagValue.Value()))
	var seq *badger.Sequence
	seq, err = inst.kv.GetSequence(prefix, generationBandwidth)
	if err != nil {
		err = eh.Errorf("unable to create sequence: %w", err)
		return
	}
	inst.inUse[tagValue] = struct{}{}
	tag := tagValue.GetTag()
	gen = &BadgerIdInternalizer{
		gen:    inst,
		seq:    seq,
		tag:    tag,
		prefix: prefix,
		maxId:  uint64(tag.GetMaxPossibleIdIncl()),
	}
	return
}

func NewBadgerIdInternalizedGenerator(storePath string) (inst *BadgerIdInternalizedGenerator, err error) {
	var kv *badger.DB
	opts := badger.DefaultOptions(storePath).WithLogger(&badger2.ZerologLoggerAdapter{})
	kv, err = badger.Open(opts)
	if err != nil {
		err = eb.Build().Str("storePath", storePath).Errorf("unable to open key value store database: %w", err)
		return
	}
	inst = &BadgerIdInternalizedGenerator{
		kv:    kv,
		inUse: make(map[identifier.TagValue]struct{}),
	}
	return
}

// Compact runs one value-log GC pass to reclaim space. Generators no longer
// compact implicitly, so callers should invoke this periodically for long-lived
// stores.
func (inst *BadgerIdInternalizedGenerator) Compact() (err error) {
	err = inst.kv.RunValueLogGC(0.5)
	if errors.Is(err, badger.ErrNoRewrite) {
		err = nil
	}
	return
}

func (inst *BadgerIdInternalizedGenerator) Close() (err error) {
	err = inst.kv.Close()
	return
}
