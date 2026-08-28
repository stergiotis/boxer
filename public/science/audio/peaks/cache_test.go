package peaks_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// The header offsets the cache-file layout documents; the tests patch them
// by hand so a silent change of the layout fails here.
const (
	offMagic     = 0
	offVersion   = 4
	offHash      = 8
	offSize      = 40
	offModTime   = 48
	offReserved  = 62
	offFrames    = 64
	headerBytes  = 80
	sampleRateAt = 56
)

func testIdentity() (id peaks.Identity) {
	for i := range id.Hash {
		id.Hash[i] = byte(i * 7)
	}
	id.SizeBytes = 8_300_000_000
	id.ModTimeUnixNano = 1_772_000_000_123_456_789
	return id
}

func TestCacheRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		format, samples, baseBin := drawCase(t)
		p := foldWhole(t, format, samples, baseBin)
		id := testIdentity()

		var buf bytes.Buffer
		require.NoError(t, p.WriteToE(&buf, id))
		require.Equal(t, int(headerBytes)+int(p.MemoryBytes()), buf.Len(),
			"a peaks file is the header plus the level arrays and nothing else")

		got, err := peaks.ReadFromE(&buf, id)
		require.NoError(t, err)
		require.Equal(t, 0, buf.Len(), "the reader must consume exactly the file")
		require.Equal(t, p.Format(), got.Format())
		require.Equal(t, p.Frames(), got.Frames())
		require.Equal(t, p.BaseBin(), got.BaseBin())
		require.Equal(t, p.Levels(), got.Levels())
		require.Equal(t, p.MemoryBytes(), got.MemoryBytes())
		require.True(t, got.IsComplete())
		require.Equal(t, p.Built(), got.Built())
		requireMatchesRef(t, got, buildRefPyramid(format, samples, baseBin))
	})
}

func TestCacheHeaderIsStable(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	p := foldWhole(t, format, genSamples(5, 2*1000), 256)
	var buf bytes.Buffer
	require.NoError(t, p.WriteToE(&buf, testIdentity()))
	hdr := buf.Bytes()[:headerBytes]
	id := testIdentity()
	require.Equal(t, "BXPK", string(hdr[offMagic:offMagic+4]))
	require.Equal(t, uint32(1), binary.LittleEndian.Uint32(hdr[offVersion:offVersion+4]))
	require.Equal(t, id.Hash[:], hdr[offHash:offHash+32])
	require.Equal(t, uint64(id.SizeBytes), binary.LittleEndian.Uint64(hdr[offSize:offSize+8]))
	require.Equal(t, uint64(id.ModTimeUnixNano), binary.LittleEndian.Uint64(hdr[offModTime:offModTime+8]))
	require.Equal(t, uint32(48000), binary.LittleEndian.Uint32(hdr[sampleRateAt:sampleRateAt+4]))
	require.Equal(t, uint16(2), binary.LittleEndian.Uint16(hdr[60:62]))
	require.Equal(t, uint16(0), binary.LittleEndian.Uint16(hdr[offReserved:offReserved+2]))
	require.Equal(t, uint64(1000), binary.LittleEndian.Uint64(hdr[offFrames:offFrames+8]))
	require.Equal(t, uint32(256), binary.LittleEndian.Uint32(hdr[72:76]))
	require.Equal(t, uint32(p.Levels()), binary.LittleEndian.Uint32(hdr[76:80]))
}

func TestCacheRefusesIncompletePyramid(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 1}
	samples := genSamples(9, 1000)
	p, err := peaks.NewPyramidE(format, 1000, 16)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.ErrorContains(t, p.WriteToE(&buf, testIdentity()), "incomplete")
	require.NoError(t, p.FoldE(samples[:500]))
	require.ErrorContains(t, p.WriteToE(&buf, testIdentity()), "incomplete")
	// Finished short of the declared length is still not a cacheable file.
	p.Finish()
	require.ErrorContains(t, p.WriteToE(&buf, testIdentity()), "incomplete")
	require.Zero(t, buf.Len())

	full := foldWhole(t, format, samples, 16)
	require.NoError(t, full.WriteToE(&buf, testIdentity()))
}

func TestCacheIdentityMismatchNamesTheField(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	p := foldWhole(t, format, genSamples(13, 2*777), 16)
	id := testIdentity()
	var good bytes.Buffer
	require.NoError(t, p.WriteToE(&good, id))

	for _, tc := range []struct {
		name  string
		want  peaks.Identity
		field string
	}{
		{name: "hash", want: func() (o peaks.Identity) { o = id; o.Hash[17] ^= 0xff; return o }(), field: "hash"},
		{name: "size", want: func() (o peaks.Identity) { o = id; o.SizeBytes++; return o }(), field: "size"},
		{name: "modTime", want: func() (o peaks.Identity) { o = id; o.ModTimeUnixNano--; return o }(), field: "modification time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := peaks.ReadFromE(bytes.NewReader(good.Bytes()), tc.want)
			require.ErrorContains(t, err, tc.field)
		})
	}

	got, err := peaks.ReadFromE(bytes.NewReader(good.Bytes()), id)
	require.NoError(t, err)
	require.Equal(t, p.Frames(), got.Frames())
}

func TestCacheRejectsMalformedFiles(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	p := foldWhole(t, format, genSamples(17, 2*600), 16)
	id := testIdentity()
	var good bytes.Buffer
	require.NoError(t, p.WriteToE(&good, id))

	patched := func(mutate func(file []byte)) (file []byte) {
		file = bytes.Clone(good.Bytes())
		mutate(file)
		return file
	}
	for _, tc := range []struct {
		name string
		file []byte
		msg  string
	}{
		{name: "empty", file: nil, msg: "header"},
		{name: "shortHeader", file: good.Bytes()[:headerBytes-1], msg: "header"},
		{
			name: "magic",
			file: patched(func(file []byte) { file[1] = 'Z' }),
			msg:  "not a peaks file",
		},
		{
			name: "version",
			file: patched(func(file []byte) { binary.LittleEndian.PutUint32(file[offVersion:], 99) }),
			msg:  "version",
		},
		{
			name: "reserved",
			file: patched(func(file []byte) { binary.LittleEndian.PutUint16(file[offReserved:], 1) }),
			msg:  "reserved",
		},
		{
			name: "baseBin",
			file: patched(func(file []byte) { binary.LittleEndian.PutUint32(file[72:], 100) }),
			msg:  "power of two",
		},
		{
			name: "levels",
			file: patched(func(file []byte) { binary.LittleEndian.PutUint32(file[76:], 3) }),
			msg:  "level count",
		},
		{
			name: "truncatedBody",
			file: good.Bytes()[:good.Len()-3],
			msg:  "body",
		},
		{
			name: "outOfRangePeak",
			file: patched(func(file []byte) { file[headerBytes+4] = 0x80 }),
			msg:  "out of range",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := peaks.ReadFromE(bytes.NewReader(tc.file), id)
			require.ErrorContains(t, err, tc.msg)
		})
	}
}

func TestCacheWriteErrorsPropagate(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	p := foldWhole(t, format, genSamples(19, 2*4000), 16)
	for _, limit := range []int{0, 10, headerBytes, headerBytes + 5} {
		err := p.WriteToE(&shortWriter{limit: limit}, testIdentity())
		require.Error(t, err, "limit %d", limit)
	}
}

// shortWriter fails once more than limit bytes have been written.
type shortWriter struct {
	limit   int
	written int
}

var _ io.Writer = (*shortWriter)(nil)

func (inst *shortWriter) Write(p []byte) (n int, err error) {
	if inst.written+len(p) > inst.limit {
		return 0, io.ErrShortWrite
	}
	inst.written += len(p)
	return len(p), nil
}
