// Package seq provides persistent, per-tag monotonic identifier.IdGeneratorI
// implementations: each hands out a dense, increasing stream of ids for one tag
// and ignores the natural key. The Badger backend leases a bandwidth-sized block
// of ids per disk write via badger.DB.GetSequence. For natural-key deduplication
// (get-or-assign) use the sibling internalized package instead.
package seq

import (
	"encoding/binary"
	"errors"
	"math/rand/v2"

	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
	badger2 "github.com/stergiotis/boxer/public/db/badger"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var _ identifier.IdGeneratorFactoryI = (*BadgerIdSequenceGenerator)(nil)
var _ identifier.IdGeneratorI = (*BadgerIdSequence)(nil)

// BadgerIdSequenceGenerator is a Badger-backed factory for per-tag sequential id
// generators; one embedded store may host many tags.
type BadgerIdSequenceGenerator struct {
	kv *badger.DB
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

func (inst *BadgerIdSequence) GetUntaggedId(naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	fresh = true
	if naturalKey != nil {
		log.Warn().Msg("natural key is ignored for sequential ids")
	}
	// Opportunistically reclaim value-log space (~1 in 65536 calls).
	if rand.Uint32()&0xffff == 0 {
		err = inst.gen.Compact()
		if err != nil {
			err = eh.Errorf("unable to compact generator: %w", err)
			return
		}
	}
	var u uint64
	u, err = inst.seq.Next()
	if err != nil {
		return
	}
	if u > inst.maxId {
		err = eb.Build().Uint64("tagValue", uint64(inst.tag.GetValue())).Uint64("untaggedId", u).Errorf("sequence value exceeds the capacity of the given tag")
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

func (inst *BadgerIdSequenceGenerator) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	if generationBandwidth == 0 {
		err = eh.Errorf("generation bandwidth is zero")
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
		kv: kv,
	}
	return
}

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
