package wavfile

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
)

func TestAppendHeaderRIFFForm(t *testing.T) {
	spec := headerSpec{
		format:   pcm.Format{SampleRate: 48000, Channels: 2},
		frames:   10,
		encoding: EncodingPCMInt,
		bits:     16,
	}
	hdr, rf64, err := appendHeader(nil, spec)
	require.NoError(t, err)
	require.False(t, rf64)
	require.Len(t, hdr, int(riffHeaderSize+chunkHeaderSize+fmtChunkSize+chunkHeaderSize))
	require.Equal(t, "RIFF", string(hdr[0:4]))
	require.Equal(t, "WAVE", string(hdr[8:12]))
	require.Equal(t, "fmt ", string(hdr[12:16]))
	require.Equal(t, "data", string(hdr[36:40]))
	dataSize := uint32(10 * 4)
	require.Equal(t, dataSize, binary.LittleEndian.Uint32(hdr[40:44]))
	require.Equal(t, uint32(4+8+16+8)+dataSize, binary.LittleEndian.Uint32(hdr[4:8]))
	require.Equal(t, formatTagPCM, binary.LittleEndian.Uint16(hdr[20:22]))
	require.Equal(t, uint16(2), binary.LittleEndian.Uint16(hdr[22:24]))
	require.Equal(t, uint32(48000), binary.LittleEndian.Uint32(hdr[24:28]))
	require.Equal(t, uint32(48000*4), binary.LittleEndian.Uint32(hdr[28:32]))
	require.Equal(t, uint16(4), binary.LittleEndian.Uint16(hdr[32:34]))
	require.Equal(t, uint16(16), binary.LittleEndian.Uint16(hdr[34:36]))
}

// Twelve hours of stereo 16-bit is 8.3 GB, which is where the RIFF size
// fields run out and the writer has to switch container on its own.
func TestAppendHeaderChoosesRF64PastFourGiB(t *testing.T) {
	frames := int64(12 * 3600 * 48000)
	spec := headerSpec{
		format:   pcm.Format{SampleRate: 48000, Channels: 2},
		frames:   frames,
		encoding: EncodingPCMInt,
		bits:     16,
	}
	hdr, rf64, err := appendHeader(nil, spec)
	require.NoError(t, err)
	require.True(t, rf64)
	require.Equal(t, "RF64", string(hdr[0:4]))
	require.Equal(t, maxUint32, binary.LittleEndian.Uint32(hdr[4:8]))
	require.Equal(t, "WAVE", string(hdr[8:12]))
	require.Equal(t, "ds64", string(hdr[12:16]))
	require.Equal(t, uint32(ds64BodySize), binary.LittleEndian.Uint32(hdr[16:20]))

	dataSize := frames * 4
	wantRiffSize := 4 + chunkHeaderSize + ds64BodySize + chunkHeaderSize + fmtChunkSize + chunkHeaderSize + dataSize
	require.Equal(t, uint64(wantRiffSize), binary.LittleEndian.Uint64(hdr[20:28]))
	require.Equal(t, uint64(dataSize), binary.LittleEndian.Uint64(hdr[28:36]))
	require.Equal(t, uint64(frames), binary.LittleEndian.Uint64(hdr[36:44]))
	require.Equal(t, uint32(0), binary.LittleEndian.Uint32(hdr[44:48]), "no chunk other than data needs a table entry")

	require.Equal(t, "fmt ", string(hdr[48:52]))
	require.Equal(t, "data", string(hdr[72:76]))
	require.Equal(t, maxUint32, binary.LittleEndian.Uint32(hdr[76:80]), "the data chunk escapes to ds64")
	require.Len(t, hdr, 80)
	// ds64's riffSize counts everything after the leading four-CC and its
	// own size field, which is the whole stream less eight bytes.
	require.Equal(t, wantRiffSize, int64(len(hdr))+dataSize-chunkHeaderSize)
}

// The RF64 read path is exercised at header scale: the ds64 sizes are the
// real (small) ones, so no gigabytes are written to check the encoder and the
// reader agree.
func TestRF64HeaderRoundTripsThroughTheReader(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	want := []float32{0, 0.5, -0.5, 0.25, 1, -1}
	src, err := pcm.NewMemSourceE(format, want)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = writeSpecE(context.Background(), &buf, headerSpec{
		format:   format,
		frames:   src.Frames(),
		encoding: EncodingIEEEFloat,
		bits:     32,
		rf64:     true,
	}, src)
	require.NoError(t, err)
	raw := buf.Bytes()
	require.Equal(t, "RF64", string(raw[0:4]))

	file, err := NewReaderE(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	require.True(t, file.IsRF64())
	require.False(t, file.IsTruncated())
	require.Equal(t, int64(3), file.Frames())
	got := make([]float32, len(want))
	n, err := file.ReadFramesAtE(context.Background(), 0, got)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, want, got)
	pcmtest.CheckSourceContract(t, file, 2000)
}

func TestAppendHeaderRejects(t *testing.T) {
	base := headerSpec{
		format:   pcm.Format{SampleRate: 48000, Channels: 2},
		frames:   10,
		encoding: EncodingPCMInt,
		bits:     16,
	}
	cases := []struct {
		name  string
		spec  headerSpec
		error string
	}{
		{name: "invalid format", spec: headerSpec{frames: 1, encoding: EncodingPCMInt, bits: 16}, error: "invalid pcm format"},
		{name: "unknown encoding", spec: func() headerSpec { s := base; s.encoding = EncodingUnknown; return s }(), error: "unsupported sample encoding"},
		{name: "unsupported width", spec: func() headerSpec { s := base; s.bits = 12; return s }(), error: "unsupported sample width"},
		{name: "float width", spec: func() headerSpec { s := base; s.encoding = EncodingIEEEFloat; s.bits = 16; return s }(), error: "unsupported sample width"},
		{name: "negative frames", spec: func() headerSpec { s := base; s.frames = -1; return s }(), error: "negative frame count"},
		{
			name:  "frame wider than block align",
			spec:  func() headerSpec { s := base; s.format.Channels = 20000; s.bits = 32; return s }(),
			error: "wider than the block-align field",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := appendHeader(nil, tc.spec)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.error)
		})
	}
}
