package chunked

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen/internalized"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// recordingWriter is a ChunkedWriterI that reassembles the payload and tracks
// the id delivered across the interrelated chunks.
type recordingWriter struct {
	id           identifier.TaggedId
	idSet        bool
	sameId       bool
	payload      bytes.Buffer
	firstAndLast int
	first        int
	intermediate int
	last         int
}

func (inst *recordingWriter) note(id identifier.TaggedId) {
	if !inst.idSet {
		inst.id = id
		inst.idSet = true
		inst.sameId = true
		return
	}
	if id != inst.id {
		inst.sameId = false
	}
}

func (inst *recordingWriter) WriteFirstChunk(id identifier.TaggedId, p []byte) (n int, err error) {
	inst.note(id)
	inst.first++
	return inst.payload.Write(p)
}

func (inst *recordingWriter) WriteIntermediateChunk(id identifier.TaggedId, p []byte, chunkIndex uint32, sizeSoFar int64) (n int, err error) {
	inst.note(id)
	inst.intermediate++
	return inst.payload.Write(p)
}

func (inst *recordingWriter) WriteLastChunk(id identifier.TaggedId, p []byte, totalChunks uint32, totalSize int64) (n int, err error) {
	inst.note(id)
	inst.last++
	return inst.payload.Write(p)
}

func (inst *recordingWriter) WriteFirstAndLastChunk(id identifier.TaggedId, p []byte) (n int, err error) {
	inst.note(id)
	inst.firstAndLast++
	return inst.payload.Write(p)
}

var _ ChunkedWriterI = (*recordingWriter)(nil)

// TestChunkerSingleChunk_WithMemInternalizer drives Chunker.Prepare with a
// concrete in-memory generator; a payload that fits one chunk takes the
// first-and-last path and gets a single generated id.
func TestChunkerSingleChunk_WithMemInternalizer(t *testing.T) {
	const tagVal = identifier.TagValue(5)
	gen, err := internalized.NewMemIdInternalizer(tagVal, 4)
	require.NoError(t, err)

	rec := &recordingWriter{}
	ch := NewChunker(64)
	key := []byte("blob-A")
	require.NoError(t, ch.Prepare(gen, key, rec))

	payload := []byte("small payload")
	_, err = ch.Write(payload)
	require.NoError(t, err)
	require.NoError(t, ch.Close())

	require.True(t, rec.idSet)
	require.True(t, rec.sameId)
	require.True(t, rec.id.IsValid())
	require.EqualValues(t, tagVal, rec.id.GetTag().GetValue())
	require.Equal(t, 1, rec.firstAndLast)
	require.Equal(t, payload, rec.payload.Bytes())

	// The id delivered to the writer is exactly the one the generator assigns
	// to this natural key.
	gotId, fresh, err := gen.GetId(key)
	require.NoError(t, err)
	require.False(t, fresh)
	require.Equal(t, rec.id, gotId)
}

// TestChunkerMultiChunk_WithMemInternalizer forces several chunks and checks the
// same generated id is carried across all of them and the payload round-trips.
func TestChunkerMultiChunk_WithMemInternalizer(t *testing.T) {
	gen, err := internalized.NewMemIdInternalizer(identifier.TagValue(9), 4)
	require.NoError(t, err)

	rec := &recordingWriter{}
	ch := NewChunker(4) // tiny chunk size to force multiple chunks
	require.NoError(t, ch.Prepare(gen, []byte("blob-multi"), rec))

	payload := []byte("0123456789abcdef") // 16 bytes
	// Write one byte at a time so bufio buffers and flushes at chunk boundaries
	// instead of taking its large-write direct path.
	for _, b := range payload {
		_, err = ch.Write([]byte{b})
		require.NoError(t, err)
	}
	require.NoError(t, ch.Close())

	require.True(t, rec.sameId)
	require.True(t, rec.id.IsValid())
	require.EqualValues(t, 9, rec.id.GetTag().GetValue())
	require.Equal(t, payload, rec.payload.Bytes())
	require.Zero(t, rec.firstAndLast, "multi-chunk stream must not use the first-and-last path")
	require.Greater(t, rec.first+rec.intermediate+rec.last, 1, "expected more than one chunk")
}

// TestChunkerEmpty_SuppressesChunksAndId verifies an empty blob emits no chunks
// and never mints an id (the generator is only consulted on real output).
func TestChunkerEmpty_SuppressesChunksAndId(t *testing.T) {
	gen, err := internalized.NewMemIdInternalizer(identifier.TagValue(1), 4)
	require.NoError(t, err)

	rec := &recordingWriter{}
	ch := NewChunker(64)
	require.NoError(t, ch.Prepare(gen, []byte("empty-blob"), rec))
	require.NoError(t, ch.Close()) // no writes

	require.False(t, rec.idSet, "no chunks should be emitted for an empty blob")
	require.Equal(t, 0, gen.Len(), "no id should be minted for an empty blob")
}

// TestChunkerReuseAcrossBlobs_WithMemInternalizer reuses one Chunker + generator
// for several blobs: distinct keys get distinct ids, and re-chunking a key
// reuses its id (generator dedup).
func TestChunkerReuseAcrossBlobs_WithMemInternalizer(t *testing.T) {
	gen, err := internalized.NewMemIdInternalizer(identifier.TagValue(2), 8)
	require.NoError(t, err)
	ch := NewChunker(64)

	write := func(key, payload []byte) identifier.TaggedId {
		rec := &recordingWriter{}
		require.NoError(t, ch.Prepare(gen, key, rec))
		_, e := ch.Write(payload)
		require.NoError(t, e)
		require.NoError(t, ch.Close())
		require.True(t, rec.idSet)
		return rec.id
	}

	idA := write([]byte("key-A"), []byte("aaa"))
	idB := write([]byte("key-B"), []byte("bbb"))
	require.NotEqual(t, idA, idB)

	// Re-chunking the same natural key reuses its id regardless of payload.
	idA2 := write([]byte("key-A"), []byte("a-different-payload"))
	require.Equal(t, idA, idA2)

	require.Equal(t, 2, gen.Len())
}
