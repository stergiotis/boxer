package viewerfixture

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ivf(payload []byte) []byte {
	b := make([]byte, ivfFirstFrame, ivfFirstFrame+len(payload))
	copy(b, ivfSignature)
	binary.LittleEndian.PutUint32(b[ivfFileHeader:], uint32(len(payload)))
	return append(b, payload...)
}

func TestIvfFirstFrame(t *testing.T) {
	payload := []byte{0x82, 0x49, 0x83, 0x42, 0x00, 0x07}
	frame, err := IvfFirstFrame(ivf(payload))
	require.NoError(t, err)
	assert.Equal(t, payload, frame)
}

// A second frame must not come with the first: the viewer is fed one frame.
func TestIvfFirstFrameStopsAtTheFrameLength(t *testing.T) {
	first := []byte{1, 2, 3}
	b := append(ivf(first), bytes.Repeat([]byte{0xff}, 40)...)
	frame, err := IvfFirstFrame(b)
	require.NoError(t, err)
	assert.Equal(t, first, frame)
}

func TestIvfFirstFrameRejects(t *testing.T) {
	for name, b := range map[string][]byte{
		"truncated":   make([]byte, 10),
		"unsigned":    append(bytes.Repeat([]byte{'X'}, 4), make([]byte, ivfFirstFrame)...),
		"empty frame": ivf(nil),
		"overlong":    func() []byte { b := ivf([]byte{1}); binary.LittleEndian.PutUint32(b[ivfFileHeader:], 99); return b }(),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := IvfFirstFrame(b)
			assert.Error(t, err)
		})
	}
}

func bmp(pixels []byte) []byte {
	b := make([]byte, bmpMinHeader, bmpMinHeader+len(pixels))
	copy(b, bmpSignature)
	binary.LittleEndian.PutUint32(b[bmpPixelsOffset:], bmpMinHeader)
	return append(b, pixels...)
}

func TestBmpDistinctPixelBytes(t *testing.T) {
	// A flat fill is what a viewer that decoded nothing leaves behind.
	flat, err := BmpDistinctPixelBytes(bmp(bytes.Repeat([]byte{0x20}, 4096)))
	require.NoError(t, err)
	assert.Equal(t, 1, flat)

	pixels := make([]byte, 4096)
	for i := range pixels {
		pixels[i] = byte(i)
	}
	drawn, err := BmpDistinctPixelBytes(bmp(pixels))
	require.NoError(t, err)
	assert.Equal(t, 256, drawn)
	assert.GreaterOrEqual(t, drawn, defaultMinDistinctBytes)
}

func TestBmpDistinctPixelBytesRejects(t *testing.T) {
	_, err := BmpDistinctPixelBytes(make([]byte, 10))
	assert.Error(t, err)

	b := bmp([]byte{1, 2, 3})
	binary.LittleEndian.PutUint32(b[bmpPixelsOffset:], uint32(len(b)+1))
	_, err = BmpDistinctPixelBytes(b)
	assert.Error(t, err)
}
