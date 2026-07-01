// Package internalized provides get-or-assign identifier.IdGeneratorI
// implementations: each maps a natural key to a stable surrogate id under one
// tag, minting a fresh id on first sight. The Badger backend persists the
// mapping in an embedded store; the in-memory (Mem) backend keeps it in a map
// and needs no external service. For key-agnostic monotonic ids use the sibling
// seq package instead.
package internalized

import (
	"iter"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// maxPreallocHint caps the pre-allocation implied by an oversized generation
// bandwidth, so a hint value cannot force a pathological map allocation.
const maxPreallocHint = 1 << 20

// ErrIdSpaceExhausted is returned once every id in a tag's body range has been
// assigned. It is wrapped with structured context, so match it with errors.Is.
var ErrIdSpaceExhausted = eh.Errorf("surrogate id space exhausted for tag")

var _ identifier.IdGeneratorI = (*MemIdInternalizer)(nil)
var _ identifier.IdGeneratorFactoryI = (*MemIdInternalizedGenerator)(nil)

// MemIdInternalizer assigns dense, monotonic surrogate ids to distinct natural
// keys under a single tag, entirely in memory, and resolves ids back to keys.
// Ids are minted from body value 1 so the zero id stays reserved as
// invalid/NULL. It is not safe for concurrent use; guard it with a sync.Mutex
// when shared across goroutines.
type MemIdInternalizer struct {
	tag     identifier.IdTag
	maxId   identifier.UntaggedId // largest body value the tag can hold (inclusive)
	offset  identifier.UntaggedId // body value of the first assigned id
	forward map[string]identifier.UntaggedId
	reverse []string // body value (offset+i) -> natural key; shares storage with the forward keys
}

// NewMemIdInternalizer returns an in-memory internalizer that mints ids under
// tagValue. estSize is a best-effort capacity hint for the number of distinct
// keys. It errors when tagValue is out of range for the active tag width.
func NewMemIdInternalizer(tagValue identifier.TagValue, estSize int) (inst *MemIdInternalizer, err error) {
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
	inst = &MemIdInternalizer{
		tag:     tag,
		maxId:   tag.GetMaxPossibleIdIncl(),
		offset:  1, // 0 is reserved as invalid/NULL
		forward: make(map[string]identifier.UntaggedId, estSize),
		reverse: make([]string, 0, estSize),
	}
	return
}

func (inst *MemIdInternalizer) GetUntaggedId(naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	if len(naturalKey) == 0 {
		err = ErrEmptyNaturalKey
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
			Errorf("cannot mint a fresh id: %w", ErrIdSpaceExhausted)
		return
	}
	s := string(naturalKey)
	inst.forward[s] = next
	inst.reverse = append(inst.reverse, s)
	untagged, fresh = next, true
	return
}

func (inst *MemIdInternalizer) GetId(naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	var untagged identifier.UntaggedId
	untagged, fresh, err = inst.GetUntaggedId(naturalKey)
	if err != nil {
		return
	}
	id = inst.tag.ComposeId(untagged)
	return
}

func (inst *MemIdInternalizer) GetTag() (tag identifier.IdTag) {
	tag = inst.tag
	return
}

// Release satisfies identifier.IdGeneratorI. The in-memory internalizer holds no
// external resources, so it retains its mapping and stays usable afterwards.
func (inst *MemIdInternalizer) Release() (err error) {
	return
}

// Len reports the number of distinct keys internalized so far.
func (inst *MemIdInternalizer) Len() (n int) {
	n = len(inst.forward)
	return
}

// Resolve maps a tagged id from this internalizer back to its natural key. found
// is false when the id carries a different tag or was never assigned. The key is
// the interned immutable string; use []byte(key) when a mutable copy is needed.
func (inst *MemIdInternalizer) Resolve(id identifier.TaggedId) (naturalKey string, found bool) {
	tag, untagged := id.Split()
	if tag != inst.tag {
		return
	}
	naturalKey, found = inst.ResolveUntagged(untagged)
	return
}

// ResolveUntagged maps a body (untagged) id back to its natural key.
func (inst *MemIdInternalizer) ResolveUntagged(untagged identifier.UntaggedId) (naturalKey string, found bool) {
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
func (inst *MemIdInternalizer) All() iter.Seq2[identifier.TaggedId, string] {
	return func(yield func(identifier.TaggedId, string) bool) {
		for i, s := range inst.reverse {
			id := inst.tag.ComposeId(inst.offset + identifier.UntaggedId(i))
			if !yield(id, s) {
				return
			}
		}
	}
}

// MemIdInternalizedGenerator is a stateless factory for in-memory internalizing
// generators. Its zero value is ready to use.
type MemIdInternalizedGenerator struct{}

// NewMemIdInternalizedGenerator returns a MemIdInternalizedGenerator.
func NewMemIdInternalizedGenerator() (inst *MemIdInternalizedGenerator) {
	inst = &MemIdInternalizedGenerator{}
	return
}

// Create returns a fresh MemIdInternalizer for tagValue. generationBandwidth is
// treated as a best-effort capacity hint (an in-memory generator reserves no id
// ranges), clamped to avoid a pathological pre-allocation.
func (inst *MemIdInternalizedGenerator) Create(tagValue identifier.TagValue, generationBandwidth uint64) (gen identifier.IdGeneratorI, err error) {
	est := min(generationBandwidth, maxPreallocHint)
	var m *MemIdInternalizer
	m, err = NewMemIdInternalizer(tagValue, int(est))
	if err != nil {
		return
	}
	gen = m
	return
}

// Close satisfies identifier.IdGeneratorFactoryI; the factory holds no resources.
func (inst *MemIdInternalizedGenerator) Close() (err error) {
	return
}
