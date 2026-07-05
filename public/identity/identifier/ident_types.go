package identifier

const TotalIdWidth = 64

// TaggedId is the concatenation of a Tag with an UntaggedId.
type TaggedId uint64

// UntaggedId is the Id part of a TaggedId.
type UntaggedId uint64

// IdTag is the Tag part of a TaggedId, kept in its original bit position.
type IdTag uint64

type TagValue uint32

type IdGeneratorI interface {
	// GetId resolves or mints the tagged id for naturalKey; fresh reports a
	// mint. Internalizing generators dedupe by key and reject an empty one;
	// sequential generators ignore the key entirely and always mint.
	GetId(naturalKey []byte) (id TaggedId, fresh bool, err error)
	// GetUntaggedId is GetId without the tag composition.
	GetUntaggedId(naturalKey []byte) (untagged UntaggedId, fresh bool, err error)

	// Release Call this to avoid waste. Calling GetId() after Release() is allowed (incurs a performance penalty)
	Release() (err error)
	GetTag() IdTag
}

type IdGeneratorFactoryI interface {
	// Create returns the generator for tagValue. generationBandwidth is a
	// backend-defined throughput/capacity hint: store-backed generators lease
	// ids from their sequence in blocks of this size per disk write, the
	// in-memory backend treats it as a pre-allocation hint. It must be
	// non-zero. Store-backed factories permit at most one generator per tag
	// (identgen.ErrTagInUse).
	Create(tagValue TagValue, generationBandwidth uint64) (gen IdGeneratorI, err error)
	Close() (err error)
}
