package decode

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// waveHeader is the first twelve bytes of a WAVE container: the container
// four-CC, a 32-bit size and the form type.
func waveHeader(container string, size uint32, form string) (head []byte) {
	head = make([]byte, 0, sniffBytes)
	head = append(head, container...)
	head = binary.LittleEndian.AppendUint32(head, size)
	head = append(head, form...)
	return head
}

func TestSniffRoutesWaveContainersToTheNativeReader(t *testing.T) {
	for _, container := range []string{"RIFF", "RF64", "BW64"} {
		t.Run(container, func(t *testing.T) {
			require.Equal(t, KindWAV, Sniff(waveHeader(container, 0xFFFFFFFF, "WAVE")))
		})
	}
}

func TestSniffRoutesEverythingElseToFfmpeg(t *testing.T) {
	cases := map[string][]byte{
		"id3 tagged mp3": append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), 0xFF, 0xFB),
		"bare mp3 frame": {0xFF, 0xFB, 0x90, 0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		"ogg":            append([]byte("OggS\x00\x02"), make([]byte, 6)...),
		"flac":           append([]byte("fLaC"), 0x00, 0x00, 0x00, 0x22, 0x10, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00),
		"riff not wave":  waveHeader("RIFF", 1024, "AVI "),
		"m4a":            append([]byte("\x00\x00\x00\x20ftypM4A "), make([]byte, 4)...),
	}
	for name, head := range cases {
		t.Run(name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(head), sniffBytes)
			require.Equal(t, KindFfmpeg, Sniff(head))
		})
	}
}

func TestSniffOnRandomBytesIsFfmpegNotAnError(t *testing.T) {
	// Unrecognisable bytes are ffmpeg's to reject: the sniff picks a decoder,
	// it does not validate the file.
	rng := rand.New(rand.NewPCG(1, 2))
	head := make([]byte, 64)
	for i := range head {
		head[i] = byte(rng.UintN(256))
	}
	require.Equal(t, KindFfmpeg, Sniff(head))
}

func TestSniffTooShortToTell(t *testing.T) {
	for n := 0; n < sniffBytes; n++ {
		require.Equal(t, KindUnknown, Sniff(waveHeader("RIFF", 0, "WAVE")[:n]), "%d bytes", n)
	}
	require.Equal(t, KindUnknown, Sniff(nil))
}

func TestKindString(t *testing.T) {
	require.Equal(t, "wav", KindWAV.String())
	require.Equal(t, "ffmpeg", KindFfmpeg.String())
	require.Equal(t, "unknown", KindUnknown.String())
	require.Equal(t, "unknown", KindE(200).String())
}
