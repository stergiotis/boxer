package wavfile

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// EncodingE is how one sample is laid out inside the data chunk.
type EncodingE uint8

const (
	// EncodingUnknown is the zero value; no successfully opened file carries
	// it.
	EncodingUnknown EncodingE = 0
	// EncodingPCMInt is WAVE_FORMAT_PCM: unsigned at 8 bits, two's-complement
	// little-endian above.
	EncodingPCMInt EncodingE = 1
	// EncodingIEEEFloat is WAVE_FORMAT_IEEE_FLOAT: little-endian binary32 or
	// binary64 nominally already in [-1, 1].
	EncodingIEEEFloat EncodingE = 2
)

// AllEncodings lists the encodings a [File] decodes and [WriteE] emits.
var AllEncodings = []EncodingE{EncodingPCMInt, EncodingIEEEFloat}

func (inst EncodingE) String() (s string) {
	switch inst {
	case EncodingPCMInt:
		return "pcmInt"
	case EncodingIEEEFloat:
		return "ieeeFloat"
	}
	return "unknown"
}

// Format tags as they appear in the fmt chunk's wFormatTag field.
const (
	formatTagPCM        uint16 = 0x0001
	formatTagIEEEFloat  uint16 = 0x0003
	formatTagExtensible uint16 = 0xFFFE
)

// Four-CCs packed big-endian, which is the order they appear in the stream,
// so a comparison is one 32-bit compare and no string escapes to the heap.
const (
	ccRIFF uint32 = 'R'<<24 | 'I'<<16 | 'F'<<8 | 'F'
	ccRF64 uint32 = 'R'<<24 | 'F'<<16 | '6'<<8 | '4'
	ccBW64 uint32 = 'B'<<24 | 'W'<<16 | '6'<<8 | '4'
	ccWAVE uint32 = 'W'<<24 | 'A'<<16 | 'V'<<8 | 'E'
	ccFmt  uint32 = 'f'<<24 | 'm'<<16 | 't'<<8 | ' '
	ccData uint32 = 'd'<<24 | 'a'<<16 | 't'<<8 | 'a'
	ccDs64 uint32 = 'd'<<24 | 's'<<16 | '6'<<8 | '4'
)

// maxUint32 is the escape value RF64 writes into a 32-bit size field whose
// real size lives in ds64, and the largest size RIFF can express.
const maxUint32 uint32 = 0xFFFFFFFF

// ksDataFormatSubtypeSuffix is bytes 4..15 of every KSDATAFORMAT_SUBTYPE_*
// GUID; the leading four bytes hold the format tag the GUID stands for.
var ksDataFormatSubtypeSuffix = [12]byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}

func readFourCC(p []byte) (cc uint32) {
	return uint32(p[0])<<24 | uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])
}

func appendFourCC(dst []byte, cc uint32) (out []byte) {
	return append(dst, byte(cc>>24), byte(cc>>16), byte(cc>>8), byte(cc))
}

func fourCCString(cc uint32) (s string) {
	p := [4]byte{byte(cc >> 24), byte(cc >> 16), byte(cc >> 8), byte(cc)}
	for i, b := range p {
		if b < 0x20 || b > 0x7E {
			p[i] = '.'
		}
	}
	return string(p[:])
}

// guidString renders a 16-byte GUID in the conventional mixed-endian text
// form so an error names something a reader can look up.
func guidString(guid []byte) (s string) {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%s",
		binary.LittleEndian.Uint32(guid[0:4]),
		binary.LittleEndian.Uint16(guid[4:6]),
		binary.LittleEndian.Uint16(guid[6:8]),
		guid[8], guid[9],
		hex.EncodeToString(guid[10:16]))
}

// subFormatTagE resolves a WAVE_FORMAT_EXTENSIBLE SubFormat GUID to the
// format tag it stands for.
func subFormatTagE(guid []byte) (tag uint16, err error) {
	if [12]byte(guid[4:16]) != ksDataFormatSubtypeSuffix {
		return 0, eb.Build().
			Str("subFormat", guidString(guid)).
			Errorf("sub-format guid %s is not a ksdataformat subtype", guidString(guid))
	}
	v := binary.LittleEndian.Uint32(guid[0:4])
	if v > uint32(^uint16(0)) {
		return 0, eb.Build().
			Str("subFormat", guidString(guid)).
			Errorf("sub-format guid %s does not name a 16-bit format tag", guidString(guid))
	}
	return uint16(v), nil
}

func formatTagOf(enc EncodingE) (tag uint16) {
	if enc == EncodingIEEEFloat {
		return formatTagIEEEFloat
	}
	return formatTagPCM
}

// validateSampleFormatE rejects the encoding/width combinations that have no
// conversion path in either direction.
func validateSampleFormatE(enc EncodingE, bits uint16) (err error) {
	switch enc {
	case EncodingPCMInt:
		switch bits {
		case 8, 16, 24, 32:
			return nil
		}
	case EncodingIEEEFloat:
		switch bits {
		case 32, 64:
			return nil
		}
	default:
		return eb.Build().
			Uint8("encoding", uint8(enc)).
			Errorf("unsupported sample encoding")
	}
	return eb.Build().
		Str("encoding", enc.String()).
		Uint16("bitsPerSample", bits).
		Errorf("unsupported width of %d bits for %s samples", bits, enc)
}
