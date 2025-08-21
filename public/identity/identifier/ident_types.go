package identifier

const TotalIdWidth = 64

// TaggedId Concatenation of Tag with UntaggedId
type TaggedId uint64

// UntaggedId Id part of TaggedId
type UntaggedId uint64

// IdTag Tag part of a TaggedId with the tag in its original position
type IdTag uint64

type TagValue uint32

type IdBatchedGeneratorI interface {
	GetIdBatch(tagValues []TagValue, naturalKeys [][][]byte, resolvedIn [][]TaggedId, freshIn [][]bool) (resolvedOut [][]TaggedId, freshOut [][]bool, err error)
	Release(tagValues []TagValue) (err error)
	GetTags() []IdTag
}

type IdBatchedGeneratorFactoryI interface {
	Create(tagValues []TagValue, generationBandwidths []uint64) (gen IdBatchedGeneratorI, err error)
	Close() (err error)
}

type IdGeneratorI interface {
	GetId(naturalKey []byte) (id TaggedId, fresh bool, err error)
	GetUntaggedId(naturalKey []byte) (untagged UntaggedId, fresh bool, err error)

	// Release Call this to avoid waste. Calling GetId() after Release() is allowed (incurs a performance penalty)
	Release() (err error)
	GetTag() IdTag
}

type IdGeneratorFactoryI interface {
	Create(tagValue TagValue, generationBandwidth uint64) (gen IdGeneratorI, err error)
	Close() (err error)
}
