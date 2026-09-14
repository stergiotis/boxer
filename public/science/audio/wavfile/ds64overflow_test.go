package wavfile

import (
	"bytes"
	"encoding/binary"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// ds64EscapedChunk appends a chunk header whose 32-bit size escapes to the
// ds64 table, followed by body as written (no pad byte).
func ds64EscapedChunk(dst []byte, id string, body []byte) (out []byte) {
	out = append(dst, id...)
	out = binary.LittleEndian.AppendUint32(out, maxUint32)
	return append(out, body...)
}

// ds64TableOverflowFixture is an RF64 stream whose second ds64 chunk is
// sized through the first one's table at nearly MaxInt64 and claims a
// full-width size table it does not carry.
func ds64TableOverflowFixture() (raw []byte) {
	table := binary.LittleEndian.AppendUint64([]byte("ds64"), uint64(math.MaxInt64-16))
	chunks := appendChunk(nil, "ds64", ds64Body(0, 0, 0, table))
	second := ds64Body(0, 0, 0, nil)
	binary.LittleEndian.PutUint32(second[24:28], maxUint32)
	chunks = ds64EscapedChunk(chunks, "ds64", second)
	chunks = appendChunk(chunks, "fmt ", fmtBody(formatTagPCM, 2, 48000, 16))
	chunks = appendChunk(chunks, "data", pcm16Body([]int16{1, -1}))
	return container("RF64", maxUint32, chunks)
}

// A ds64 table entry is untrusted: a size near MaxInt64 must not wrap the
// body+size fit check. Before the check was overflow-safe, a second ds64
// chunk sized through the first one's table passed it and declared a
// 2^32-1 entry table, which the parser allocated up front — 48 GiB of
// slices from a stream of a few hundred bytes.
func TestReadRF64Ds64TableSizeNearMaxInt64(t *testing.T) {
	raw := ds64TableOverflowFixture()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	runtime.ReadMemStats(&after)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chunk extends past the end of the stream")
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

// The same wrap for an fmt chunk sized through the table.
func TestReadRF64FmtSizeNearMaxInt64(t *testing.T) {
	table := binary.LittleEndian.AppendUint64([]byte("fmt "), uint64(math.MaxInt64))
	chunks := appendChunk(nil, "ds64", ds64Body(0, 4, 1, table))
	chunks = ds64EscapedChunk(chunks, "fmt ", fmtBody(formatTagPCM, 2, 48000, 16))
	chunks = appendChunk(chunks, "data", pcm16Body([]int16{1, -1}))
	raw := container("RF64", maxUint32, chunks)

	_, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "chunk extends past the end of the stream")
}

// A data chunk may declare more than the stream holds, and the reader keeps
// what exists. An odd declared size near MaxInt64 must take that path too,
// rather than wrapping the pad-byte advance negative and failing the walk.
func TestReadRF64TruncatedDataSizeNearMaxInt64(t *testing.T) {
	samples := []int16{1, -1, 300, -300}
	for _, declared := range []uint64{math.MaxInt64 - 1, math.MaxInt64} {
		raw := rf64Fixture(t, "RF64", declared, samples, nil)
		file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
		require.NoError(t, err, "declared %d", declared)
		require.True(t, file.IsTruncated())
		require.Equal(t, int64(2), file.Frames())
	}
}
