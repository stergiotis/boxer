package recordstoretest

import (
	"github.com/stergiotis/boxer/public/identity/identgen/leased"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/memalloc"
	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// FactsRowTag is the tag value of the facts table's row id stream
// (vocab.IdStreamFactsRow), restated here so that a test below the runtime
// can build a key stream under it.
const FactsRowTag identifier.TagValue = 57314

// KeyStream is an id generator for the Keys of a store under test that
// mints its key: tagged ids under tagValue, counted in process. It stands
// in for a stream leased from a server. It panics on a tag value the
// generator refuses, which is a mistake in the test.
func KeyStream(tagValue identifier.TagValue) identifier.IdGeneratorI {
	seq, err := leased.NewSequence(memalloc.NewAllocator(), tagValue, 1024)
	if err != nil {
		panic("recordstoretest.KeyStream: " + err.Error())
	}
	return seq
}

// KeyedStream is a keyed id generator for the Keys of a store under test
// whose key names an entity: the same natural key resolves to the same
// tagged id, for the life of the value. It stands in for the directory a
// server keeps. It panics as [KeyStream] does.
func KeyedStream(tagValue identifier.TagValue) identifier.IdGeneratorI {
	gen, err := mem.NewIdInternalizer(tagValue, 64)
	if err != nil {
		panic("recordstoretest.KeyedStream: " + err.Error())
	}
	return gen
}
