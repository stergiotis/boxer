package wavfile

import (
	"encoding/binary"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

const (
	riffHeaderSize  int64 = 12
	chunkHeaderSize int64 = 8
	// fmtChunkSize is the WAVEFORMAT body a canonical writer emits; the
	// reader accepts the longer WAVEFORMATEX and WAVEFORMATEXTENSIBLE bodies
	// too.
	fmtChunkSize int64 = 16
	// ds64BodySize is the ds64 body without its size table.
	ds64BodySize int64 = 28
	// ds64TableEntrySize is one four-CC plus one 64-bit size.
	ds64TableEntrySize int64 = 12
	// maxRIFFSize is the largest byte count RIFF's 32-bit size field can
	// express; a stream whose header would exceed it is written as RF64.
	maxRIFFSize int64 = int64(maxUint32)
)

// headerSpec is the shape a canonical header describes. It is the input of
// both [appendHeader] and [WriteE], so the RF64 branch can be exercised at
// header scale without writing gigabytes of samples.
type headerSpec struct {
	format   pcm.Format
	frames   int64
	encoding EncodingE
	bits     uint16
	// rf64 asks for the RF64 form even when the byte counts would fit RIFF's
	// 32-bit fields, which is what a BW64 producer does unconditionally. The
	// RF64 form is used regardless when they do not fit.
	rf64 bool
}

func (inst headerSpec) blockAlign() (n int64) {
	return int64(inst.format.Channels) * int64(inst.bits/8)
}

func (inst headerSpec) dataSize() (n int64) {
	return inst.frames * inst.blockAlign()
}

// appendHeader appends a canonical little-endian WAVE header — a 16-byte fmt
// chunk and the data chunk's own header, preceded by ds64 in the RF64 form —
// and reports whether the RF64 form was chosen. Exactly dataSize() bytes of
// samples follow, plus a pad byte when that count is odd.
func appendHeader(dst []byte, spec headerSpec) (out []byte, rf64 bool, err error) {
	err = spec.format.ValidateE()
	if err != nil {
		return dst, false, err
	}
	err = validateSampleFormatE(spec.encoding, spec.bits)
	if err != nil {
		return dst, false, err
	}
	if spec.frames < 0 {
		return dst, false, eb.Build().
			Int64("frames", spec.frames).
			Errorf("negative frame count")
	}
	blockAlign := spec.blockAlign()
	if blockAlign > int64(^uint16(0)) {
		return dst, false, eb.Build().
			Int64("blockAlign", blockAlign).
			Uint16("channels", spec.format.Channels).
			Errorf("frame of %d bytes is wider than the block-align field", blockAlign)
	}
	dataSize := spec.dataSize()
	pad := dataSize & 1
	// The form type plus the two chunks that always exist.
	riffSize := 4 + chunkHeaderSize + fmtChunkSize + chunkHeaderSize + dataSize + pad
	rf64 = spec.rf64 || riffSize > maxRIFFSize
	if rf64 {
		riffSize += chunkHeaderSize + ds64BodySize
	}

	out = dst
	if rf64 {
		out = appendFourCC(out, ccRF64)
		out = binary.LittleEndian.AppendUint32(out, maxUint32)
		out = appendFourCC(out, ccWAVE)
		out = appendFourCC(out, ccDs64)
		out = binary.LittleEndian.AppendUint32(out, uint32(ds64BodySize))
		out = binary.LittleEndian.AppendUint64(out, uint64(riffSize))
		out = binary.LittleEndian.AppendUint64(out, uint64(dataSize))
		out = binary.LittleEndian.AppendUint64(out, uint64(spec.frames))
		// No table: every chunk this writer emits other than data fits in a
		// 32-bit size field.
		out = binary.LittleEndian.AppendUint32(out, 0)
	} else {
		out = appendFourCC(out, ccRIFF)
		out = binary.LittleEndian.AppendUint32(out, uint32(riffSize))
		out = appendFourCC(out, ccWAVE)
	}

	out = appendFourCC(out, ccFmt)
	out = binary.LittleEndian.AppendUint32(out, uint32(fmtChunkSize))
	out = binary.LittleEndian.AppendUint16(out, formatTagOf(spec.encoding))
	out = binary.LittleEndian.AppendUint16(out, spec.format.Channels)
	out = binary.LittleEndian.AppendUint32(out, spec.format.SampleRate)
	out = binary.LittleEndian.AppendUint32(out, uint32(int64(spec.format.SampleRate)*blockAlign))
	out = binary.LittleEndian.AppendUint16(out, uint16(blockAlign))
	out = binary.LittleEndian.AppendUint16(out, spec.bits)

	out = appendFourCC(out, ccData)
	if rf64 {
		out = binary.LittleEndian.AppendUint32(out, maxUint32)
	} else {
		out = binary.LittleEndian.AppendUint32(out, uint32(dataSize))
	}
	return out, rf64, nil
}
