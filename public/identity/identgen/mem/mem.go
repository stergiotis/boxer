// Package mem is the dependency-free, in-memory get-or-assign
// identifier.IdGeneratorI: it maps a natural key to a stable surrogate id in a
// Go map and resolves ids back to keys. It pulls in no storage engine, so unlike
// the Badger-backed internalized/seq packages it stays WASM-compilable. For a
// durable mapping use identgen/internalized instead.
package mem

import (
	"iter"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// maxPreallocHint caps the pre-allocation implied by an oversized generation
// bandwidth, so a hint value cannot force a pathological map allocation.
const maxPreallocHint = 1 << 20

var _ identifier.IdGeneratorI = (*IdInternalizer)(nil)
var _ identifier.IdGeneratorFactoryI = (*IdInternalizedGenerator)(nil)

// IdInternalizer assigns dense, monotonic surrogate ids to distinct natural keys
// under a single tag, entirely in memory, and resolves ids back to keys. Ids are
// minted from body value 1 so the zero id stays reserved as invalid/NULL. It is
// not safe for concurrent use; guard it with a sync.Mutex when shared across
// goroutines.
type IdInternalizer struct {
	tag     identifier.IdTag
	maxId   identifier.UntaggedId // largest body value the tag can hold (inclusive)
	offset  identifier.UntaggedId // body value of the first assigned id
	forward map[string]identifier.UntaggedId
	reverse []string // body value (offset+i) -> natural key; shares storage with the forward keys
}

// NewIdInternalizer returns an in-memory internalizer that mints ids under
// tagValue. estSize is a best-effort capacity hint for the number of distinct
// keys. It errors when tagValue is out of range for the active tag width.
func NewIdInternalizer(tagValue identifier.TagValue, estSize int) (inst *IdInternalizer, err error) {
	if !tagValue.IsValid() {
		err = eb.Build().
			Uint64("tagValue", uint64(tagValue)).
			Uint64("maxTagValue", uint64(identifier.MaxTagValue)).
			Errorf("tag value out of range for the active tag width")
		return
	}
	if estSize < 0 {
		estSize = 0
	}
	tag := tagValue.GetTag()
	inst = &IdInternalizer{
		tag:     tag,
		maxId:   tag.GetMaxPossibleIdIncl(),
		offset:  1, // 0 is reserved as invalid/NULL
		forward: make(map[string]identifier.UntaggedId, estSize),
		reverse: make([]string, 0, estSize),
	}
	return
}

func (inst *IdInternalizer) GetUntaggedId(naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	if len(naturalKey) == 0 {
		err = identgen.ErrEmptyNaturalKey
		return
	}
	if u, has := inst.forward[string(naturalKey)]; has {
		untagged = u
		return
	}
	next := inst.offset + identifier.UntaggedId(len(inst.forward))
	if next > inst.maxId {
		err = eb.Build().
			Uint64("maxIdIncl", uint64(inst.maxId)).
			Uint64("tagValue", uint64(inst.tag.GetValue())).
			Errorf("cannot mint a fresh id: %w", identgen.ErrIdSpaceExhausted)
		return
	}
	s := string(naturalKey)
	inst.forward[s] = next
	inst.reverse = append(inst.reverse, s)
	untagged, fresh = next, true
	return
}

func (inst *IdInternalizer) GetId(naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	var untagged identifier.UntaggedId
	untagged, fresh, err = inst.GetUntaggedId(naturalKey)
	if err != nil {
		return
	}
	id = inst.tag.ComposeId(untagged)
	return
}

func (inst *IdInternalizer) GetTag() (tag identifier.IdTag) {
	tag = inst.tag
	return
}

// Release satisfies identifier.IdGeneratorI. The in-memory internalizer holds no
// external resources, so it retains its mapping and stays usable afterwards.
func (inst *IdInternalizer) Release() (err error) {
	return
}

// Len reports the number of distinct keys internalized so far.
func (inst *IdInternalizer) Len() (n int) {
	n = len(inst.forward)
	return
}

// Resolve maps a tagged id from this internalizer back to its natural key. found
// is false when the id carries a different tag or was never assigned. The key is
// the interned immutable string; use []byte(key) when a mutable copy is needed.
func (inst *IdInternalizer) Resolve(id identifier.TaggedId) (naturalKey string, found bool) {
	tag, untagged := id.Split()
	if tag != inst.tag {
		return
	}
	naturalKey, found = inst.ResolveUntagged(untagged)
	return
}

// ResolveUntagged maps a body (untagged) id back to its natural key.
func (inst *IdInternalizer) ResolveUntagged(untagged identifier.UntaggedId) (naturalKey string, found bool) {
	if untagged < inst.offset {
		return
	}
	idx := int(untagged - inst.offset)
	if idx >= len(inst.reverse) {
		return
	}
	naturalKey = inst.reverse[idx]
	found = true
	return
}

// All iterates every (id, naturalKey) pair in assignment order.
func (inst *IdInternalizer) All() iter.Seq2[identifier.TaggedId, string] {
	return func(yield func(identifier.TaggedId, string) bool) {
		for i, s := range inst.reverse {
			id := inst.tag.ComposeId(inst.offset + identifier.UntaggedId(i))
			if !yield(id, s) {
				return
			}
		}
	}
}

// IdInternalizedGenerator is a stateless factory for in-memory internalizing
// generators. Its zero value is ready to use.
type IdInternalizedGenerator struct{}

// NewIdInternalizedGenerator returns an IdInternalizedGenerator.
func NewIdInternalizedGenerator() (inst *IdInternalizedGenerator) {
	inst = &IdInternalizedGenerator{}
	return
}

// Create returns a fresh IdInternalizer for tagValue. generationBandwidth is
// treated as a best-effort capacity hint (an in-memory generator reserves no id
// ranges), clamped to avoid a pathological pre-allocation.
func (inst *IdInternalizedGenerator) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	est := min(generationBandwidth, maxPreallocHint)
	var m *IdInternalizer
	m, err = NewIdInternalizer(tagValue, int(est))
	if err != nil {
		return
	}
	gen = m
	return
}

// Close satisfies identifier.IdGeneratorFactoryI; the factory holds no resources.
func (inst *IdInternalizedGenerator) Close() (err error) {
	return
}
