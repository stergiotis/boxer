package adhocdata

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/sealed"
)

// zstdZeros is a compressed stream of batches of n zero int64s: a few
// hundred bytes that decode to batches*n*8.
func zstdZeros(t *testing.T, batches int, n int) []byte {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Int64Builder).AppendValues(make([]int64, n), nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithZstd())
	for range batches {
		require.NoError(t, w.Write(rec))
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// A compressed stream is bounded by what it decodes to, not by its own
// length: batch after batch of zeros stops at the limit.
func TestSealStreamBoundsDecodedBytes(t *testing.T) {
	stream := zstdZeros(t, 64, 1<<14) // 8 MiB decoded
	require.Less(t, len(stream), 64<<10)

	f, err := sealed.CreateIn(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, _, _, err = sealStreamCapped(f, stream, 1<<20)
	assert.ErrorContains(t, err, "per-dataset quota")

	ok, err := sealed.CreateIn(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = ok.Close() }()
	_, _, rows, err := sealStreamCapped(ok, stream, 64<<20)
	require.NoError(t, err)
	assert.EqualValues(t, 64<<14, rows)
}

// A buffer declaring a huge uncompressed length is refused before it is
// allocated.
func TestPublishRefusesDeclaredHugeBuffer(t *testing.T) {
	const n = 1000
	stream := zstdZeros(t, 1, n)
	var want, huge [8]byte
	binary.LittleEndian.PutUint64(want[:], n*8)
	binary.LittleEndian.PutUint64(huge[:], 2<<30)
	require.Equal(t, 1, bytes.Count(stream, want[:]), "one buffer declares its decoded length")
	stream = bytes.Replace(stream, want[:], huge[:], 1)

	svc := newTestService(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := svc.Publish(PublishInput{Alias: "bomb", ArrowIPCStream: stream})
	runtime.ReadMemStats(&after)
	assert.ErrorContains(t, err, "per-dataset quota")
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(PerDatasetMaxBytes), "the declared length was not allocated")
}

// A compressed stream that barely compresses decodes to about its own
// length; the message bodies the reader allocates must not count against
// the decoded bytes' share of the budget, or such a stream is refused
// well under the quota.
func TestSealStreamAcceptsIncompressibleCompressedStream(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	vals := make([]int64, 80000) // 640 KB decoded
	x := uint64(0x9e3779b97f4a7c15)
	for i := range vals {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		vals[i] = int64(x)
	}
	rb.Field(0).(*array.Int64Builder).AppendValues(vals, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithZstd())
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())

	f, err := sealed.CreateIn(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, _, rows, err := sealStreamCapped(f, buf.Bytes(), 1<<20)
	require.NoError(t, err)
	assert.EqualValues(t, len(vals), rows)
}
