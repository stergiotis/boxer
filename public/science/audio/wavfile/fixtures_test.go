package wavfile

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
)

// container assembles a RIFF-shaped stream with an explicit size field, so a
// fixture can write the RF64 escape or a deliberately wrong size.
func container(form string, sizeField uint32, chunks []byte) (out []byte) {
	out = make([]byte, 0, int(riffHeaderSize)+len(chunks))
	out = append(out, form...)
	out = binary.LittleEndian.AppendUint32(out, sizeField)
	out = append(out, "WAVE"...)
	out = append(out, chunks...)
	return out
}

func riffContainer(chunks []byte) (out []byte) {
	return container("RIFF", uint32(4+len(chunks)), chunks)
}

func appendChunk(dst []byte, id string, body []byte) (out []byte) {
	out = append(dst, id...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body)))
	out = append(out, body...)
	if len(body)&1 == 1 {
		out = append(out, 0)
	}
	return out
}

func fmtBody(tag uint16, channels uint16, rate uint32, bits uint16) (body []byte) {
	blockAlign := channels * bits / 8
	body = make([]byte, 0, 40)
	body = binary.LittleEndian.AppendUint16(body, tag)
	body = binary.LittleEndian.AppendUint16(body, channels)
	body = binary.LittleEndian.AppendUint32(body, rate)
	body = binary.LittleEndian.AppendUint32(body, rate*uint32(blockAlign))
	body = binary.LittleEndian.AppendUint16(body, blockAlign)
	body = binary.LittleEndian.AppendUint16(body, bits)
	return body
}

func fmtBodyExtensible(channels uint16, rate uint32, bits uint16, validBits uint16, guid []byte) (body []byte) {
	body = fmtBody(formatTagExtensible, channels, rate, bits)
	body = binary.LittleEndian.AppendUint16(body, 22)
	body = binary.LittleEndian.AppendUint16(body, validBits)
	// FRONT_LEFT | FRONT_RIGHT.
	body = binary.LittleEndian.AppendUint32(body, 0x3)
	body = append(body, guid...)
	return body
}

func subFormatGUID(tag uint16) (guid []byte) {
	guid = make([]byte, 0, 16)
	guid = binary.LittleEndian.AppendUint32(guid, uint32(tag))
	guid = append(guid, ksDataFormatSubtypeSuffix[:]...)
	return guid
}

func pcm16Body(samples []int16) (out []byte) {
	out = make([]byte, 0, 2*len(samples))
	for _, s := range samples {
		out = binary.LittleEndian.AppendUint16(out, uint16(s))
	}
	return out
}

func pcm24Body(samples []int32) (out []byte) {
	out = make([]byte, 0, 3*len(samples))
	for _, s := range samples {
		out = append(out, byte(s), byte(s>>8), byte(s>>16))
	}
	return out
}

func readAll(t *testing.T, file *File) (samples []float32) {
	t.Helper()
	total := int(file.Frames()) * int(file.Format().Channels)
	samples = make([]float32, total)
	if total == 0 {
		return samples
	}
	n, err := file.ReadFramesAtE(context.Background(), 0, samples)
	require.NoError(t, err)
	require.Equal(t, int(file.Frames()), n)
	return samples
}

func TestReadExtensiblePCMGUID(t *testing.T) {
	samples := []int16{0, 16384, -16384, 32767, -32768, 1024}
	chunks := appendChunk(nil, "fmt ", fmtBodyExtensible(2, 48000, 16, 16, subFormatGUID(formatTagPCM)))
	chunks = appendChunk(chunks, "data", pcm16Body(samples))
	raw := riffContainer(chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, EncodingPCMInt, file.Encoding())
	require.Equal(t, uint16(16), file.BitsPerSample())
	require.Equal(t, uint16(16), file.ValidBitsPerSample())
	require.Equal(t, pcm.Format{SampleRate: 48000, Channels: 2}, file.Format())
	require.Equal(t, int64(3), file.Frames())
	require.False(t, file.IsRF64())
	require.False(t, file.IsTruncated())

	got := readAll(t, file)
	for i, s := range samples {
		require.InDelta(t, float64(s)/32768, float64(got[i]), 1e-9, "sample %d", i)
	}
	pcmtest.CheckSourceContract(t, file, 2000)
	require.NoError(t, file.CloseE())
}

func TestReadExtensibleFloatGUID(t *testing.T) {
	want := []float32{0, 0.25, -0.5, 1, -1, 0.125}
	body := appendSamples(make([]byte, 0, 4*len(want)), want, EncodingIEEEFloat, 32)
	chunks := appendChunk(nil, "fmt ", fmtBodyExtensible(3, 44100, 32, 32, subFormatGUID(formatTagIEEEFloat)))
	chunks = appendChunk(chunks, "data", body)
	raw := riffContainer(chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, EncodingIEEEFloat, file.Encoding())
	require.Equal(t, int64(2), file.Frames())
	require.Equal(t, want, readAll(t, file))
	pcmtest.CheckSourceContract(t, file, 2000)
}

// A container wider than the valid bits is legal and left-justified, so the
// container's range stays the conversion scale.
func TestReadExtensibleNarrowValidBits(t *testing.T) {
	samples := []int32{0, 1 << 22, -(1 << 22), 8388607}
	chunks := appendChunk(nil, "fmt ", fmtBodyExtensible(1, 96000, 24, 20, subFormatGUID(formatTagPCM)))
	chunks = appendChunk(chunks, "data", pcm24Body(samples))
	raw := riffContainer(chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, uint16(24), file.BitsPerSample())
	require.Equal(t, uint16(20), file.ValidBitsPerSample())
	require.Equal(t, int64(4), file.Frames())
	got := readAll(t, file)
	for i, s := range samples {
		require.InDelta(t, float64(s)/8388608, float64(got[i]), 1e-9, "sample %d", i)
	}
}

// Odd-sized chunks on both sides of fmt, and a chunk id the walk does not
// know, must all be skipped over the pad byte.
func TestReadTolerateUnknownAndOddSizedChunks(t *testing.T) {
	samples := []int16{100, -100, 200, -200}
	chunks := appendChunk(nil, "JUNK", []byte("padme"))
	chunks = appendChunk(chunks, "LIST", []byte("INFO"))
	chunks = appendChunk(chunks, "fmt ", fmtBody(formatTagPCM, 1, 8000, 16))
	chunks = appendChunk(chunks, "bext", make([]byte, 7))
	chunks = appendChunk(chunks, "data", pcm16Body(samples))
	raw := riffContainer(chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, int64(4), file.Frames())
	got := readAll(t, file)
	for i, s := range samples {
		require.InDelta(t, float64(s)/32768, float64(got[i]), 1e-9, "sample %d", i)
	}
	pcmtest.CheckSourceContract(t, file, 2000)
}

func TestReadTruncatedDataChunk(t *testing.T) {
	samples := make([]int16, 20)
	for i := range samples {
		samples[i] = int16(i * 100)
	}
	chunks := appendChunk(nil, "fmt ", fmtBody(formatTagPCM, 1, 8000, 16))
	chunks = append(chunks, "data"...)
	// The data chunk claims a hundred times the bytes that follow it.
	chunks = binary.LittleEndian.AppendUint32(chunks, 4000)
	chunks = append(chunks, pcm16Body(samples)...)
	raw := riffContainer(chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.True(t, file.IsTruncated())
	require.Equal(t, int64(20), file.Frames())
	got := readAll(t, file)
	for i, s := range samples {
		require.InDelta(t, float64(s)/32768, float64(got[i]), 1e-9, "sample %d", i)
	}
	pcmtest.CheckSourceContract(t, file, 2000)
}

// ds64 body: riffSize, dataSize, sampleCount, then the size table.
func ds64Body(riffSize uint64, dataSize uint64, sampleCount uint64, table []byte) (out []byte) {
	out = make([]byte, 0, int(ds64BodySize)+len(table))
	out = binary.LittleEndian.AppendUint64(out, riffSize)
	out = binary.LittleEndian.AppendUint64(out, dataSize)
	out = binary.LittleEndian.AppendUint64(out, sampleCount)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(table)/int(ds64TableEntrySize)))
	out = append(out, table...)
	return out
}

func rf64Fixture(t *testing.T, form string, declaredDataSize uint64, samples []int16, table []byte) (raw []byte) {
	t.Helper()
	data := pcm16Body(samples)
	chunks := appendChunk(nil, "ds64", ds64Body(0, declaredDataSize, uint64(len(samples)/2), table))
	chunks = appendChunk(chunks, "fmt ", fmtBody(formatTagPCM, 2, 48000, 16))
	chunks = append(chunks, "data"...)
	chunks = binary.LittleEndian.AppendUint32(chunks, maxUint32)
	chunks = append(chunks, data...)
	return container(form, maxUint32, chunks)
}

func TestReadRF64EscapedDataSize(t *testing.T) {
	samples := []int16{1, -1, 300, -300, 4000, -4000, 32767, -32768}
	for _, form := range []string{"RF64", "BW64"} {
		t.Run(form, func(t *testing.T) {
			raw := rf64Fixture(t, form, uint64(2*len(samples)), samples, nil)
			file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
			require.NoError(t, err)
			require.True(t, file.IsRF64())
			require.False(t, file.IsTruncated())
			require.Equal(t, int64(4), file.Frames())
			got := readAll(t, file)
			for i, s := range samples {
				require.InDelta(t, float64(s)/32768, float64(got[i]), 1e-9, "sample %d", i)
			}
			pcmtest.CheckSourceContract(t, file, 2000)
		})
	}
}

// ds64 may declare the size of a long recording whose tail never arrived.
func TestReadRF64Ds64DeclaresMoreThanTheStreamHolds(t *testing.T) {
	samples := []int16{1, -1, 300, -300, 4000, -4000, 32767, -32768}
	raw := rf64Fixture(t, "RF64", 12*3600*48000*4, samples, nil)
	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.True(t, file.IsRF64())
	require.True(t, file.IsTruncated())
	require.Equal(t, int64(4), file.Frames())
	pcmtest.CheckSourceContract(t, file, 2000)
}

// A chunk other than data may also escape to a 64-bit size, and its entry
// lives in the ds64 table.
func TestReadRF64TableSizedChunk(t *testing.T) {
	samples := []int16{7, -7, 9, -9}
	junk := []byte("metadata")
	table := append([]byte("JUNK"), make([]byte, 8)...)
	binary.LittleEndian.PutUint64(table[4:12], uint64(len(junk)))

	chunks := appendChunk(nil, "ds64", ds64Body(0, uint64(2*len(samples)), 2, table))
	chunks = append(chunks, "JUNK"...)
	chunks = binary.LittleEndian.AppendUint32(chunks, maxUint32)
	chunks = append(chunks, junk...)
	chunks = appendChunk(chunks, "fmt ", fmtBody(formatTagPCM, 2, 48000, 16))
	chunks = append(chunks, "data"...)
	chunks = binary.LittleEndian.AppendUint32(chunks, maxUint32)
	chunks = append(chunks, pcm16Body(samples)...)
	raw := container("RF64", maxUint32, chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, int64(2), file.Frames())
	pcmtest.CheckSourceContract(t, file, 2000)
}

func TestReadZeroFrameFixture(t *testing.T) {
	chunks := appendChunk(nil, "fmt ", fmtBody(formatTagPCM, 2, 48000, 16))
	chunks = appendChunk(chunks, "data", nil)
	raw := riffContainer(chunks)

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.Equal(t, int64(0), file.Frames())
	require.False(t, file.IsTruncated())
	pcmtest.CheckSourceContract(t, file, 2000)
}

func TestReadRejects(t *testing.T) {
	pcmFmt := fmtBody(formatTagPCM, 2, 48000, 16)
	// An RF64 stream whose data chunk escapes to a 64-bit size but that
	// carries no ds64 chunk to resolve it.
	rf64NoDs64 := appendChunk(nil, "fmt ", pcmFmt)
	rf64NoDs64 = append(rf64NoDs64, "data"...)
	rf64NoDs64 = binary.LittleEndian.AppendUint32(rf64NoDs64, maxUint32)
	rf64NoDs64 = append(rf64NoDs64, make([]byte, 16)...)

	cases := []struct {
		name  string
		raw   []byte
		error string
	}{
		{
			name:  "unsupported format tag",
			raw:   riffContainer(appendChunk(appendChunk(nil, "fmt ", fmtBody(0x0011, 2, 48000, 4)), "data", make([]byte, 16))),
			error: "0x0011",
		},
		{
			name:  "unsupported extensible sub-format",
			raw:   riffContainer(appendChunk(appendChunk(nil, "fmt ", fmtBodyExtensible(2, 48000, 16, 16, subFormatGUID(0x0011))), "data", make([]byte, 16))),
			error: "0x0011",
		},
		{
			name:  "sub-format guid is not a ksdataformat subtype",
			raw:   riffContainer(appendChunk(appendChunk(nil, "fmt ", fmtBodyExtensible(2, 48000, 16, 16, make([]byte, 16))), "data", make([]byte, 16))),
			error: "ksdataformat subtype",
		},
		{
			name:  "unsupported bit depth",
			raw:   riffContainer(appendChunk(appendChunk(nil, "fmt ", fmtBody(formatTagPCM, 2, 48000, 64)), "data", make([]byte, 16))),
			error: "unsupported width of 64 bits",
		},
		{
			name:  "not a riff container",
			raw:   container("JUNK", 4, nil),
			error: "not a riff container",
		},
		{
			name:  "not a wave",
			raw:   append(append([]byte("RIFF"), 4, 0, 0, 0), "AVI "...),
			error: "not a wave",
		},
		{
			name:  "no fmt chunk",
			raw:   riffContainer(appendChunk(nil, "data", make([]byte, 16))),
			error: "no fmt chunk",
		},
		{
			name:  "no data chunk",
			raw:   riffContainer(appendChunk(nil, "fmt ", pcmFmt)),
			error: "no data chunk",
		},
		{
			name:  "too short for a header",
			raw:   []byte("RIFF"),
			error: "too short to hold a riff header",
		},
		{
			name:  "rf64 escape with no ds64",
			raw:   container("RF64", maxUint32, rf64NoDs64),
			error: "no ds64 chunk preceded it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReaderE(bytes.NewReader(tc.raw), int64(len(tc.raw)))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.error)
		})
	}
}

func TestOpenEClosesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	src, err := pcm.NewMemSourceE(pcm.Format{SampleRate: 8000, Channels: 1}, []float32{0, 0.5, -0.5, 1})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, WriteE(context.Background(), &buf, src.Format(), EncodingPCMInt, 16, src))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	file, err := OpenE(path)
	require.NoError(t, err)
	require.Equal(t, int64(4), file.Frames())
	pcmtest.CheckSourceContract(t, file, 2000)
	require.NoError(t, file.CloseE())
	// CloseE is idempotent, and the second call must not close a file it no
	// longer owns.
	require.NoError(t, file.CloseE())

	_, err = OpenE(filepath.Join(t.TempDir(), "absent.wav"))
	require.Error(t, err)
}
