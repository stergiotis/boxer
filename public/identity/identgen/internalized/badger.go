package internalized

import (
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"sync"

	"github.com/dgraph-io/badger/v4"
	badger2 "github.com/stergiotis/boxer/public/db/badger"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrEmptyNaturalKey is returned when a nil or zero-length natural key is passed
// to an internalizing generator (an internalizer must dedupe by key).
var ErrEmptyNaturalKey = eh.Errorf("natural key is empty")

var _ identifier.IdGeneratorFactoryI = (*BadgerIdInternalizedGenerator)(nil)
var _ identifier.IdGeneratorI = (*BadgerIdInternalizer)(nil)

// BadgerIdInternalizedGenerator is a Badger-backed factory for per-tag
// internalizing id generators; a single embedded store may host many tags.
type BadgerIdInternalizedGenerator struct {
	kv *badger.DB
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

func (inst *BadgerIdInternalizer) GetUntaggedId(naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	if len(naturalKey) == 0 {
		err = ErrEmptyNaturalKey
		return
	}
	inst.lock.Lock()
	defer inst.lock.Unlock()

	// Opportunistically reclaim value-log space (~1 in 65536 calls).
	if rand.Uint32()&0xffff == 0 {
		err = inst.gen.Compact()
		if err != nil {
			err = eh.Errorf("unable to compact generator: %w", err)
			return
		}
	}

	// Mapping keys are tag-scoped (prefix ++ naturalKey) so distinct tags sharing
	// one store never collide; the sequence counter lives at the 8-byte prefix
	// alone, which is length-disjoint from every (non-empty) mapping key.
	key := make([]byte, 0, len(inst.prefix)+len(naturalKey))
	key = append(key, inst.prefix...)
	key = append(key, naturalKey...)

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
	u, err = inst.seq.Next()
	if err != nil {
		err = eh.Errorf("unable to obtain next sequence value: %w", err)
		return
	}
	if u > inst.maxId {
		err = eb.Build().Uint64("tagValue", uint64(inst.tag.GetValue())).Uint64("untaggedId", u).Errorf("sequence value exceeds the capacity of the given tag")
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

func (inst *BadgerIdInternalizedGenerator) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	if generationBandwidth == 0 {
		err = eh.Errorf("generation bandwidth is zero")
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
		kv: kv,
	}
	return
}

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
