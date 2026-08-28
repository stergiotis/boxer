package wavfile

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// File decodes frames out of a RIFF/WAVE or RF64/BW64 stream on demand
// (ADR-0208 §SD5). Opening reads the header; samples are converted per
// request from the byte offset of the data chunk, so a file larger than
// memory costs only the window a caller asks for.
//
// A File is usable from one goroutine at a time, like every pcm.SourceI: the
// scratch buffer and the underlying reader position are shared state.
type File struct {
	ra     io.ReaderAt
	closer io.Closer

	// scratch holds the raw bytes of the frames a read asks for. It grows to
	// the largest request and is then reused (CODINGSTANDARDS Memory §
	// Reuse).
	scratch []byte

	// The ds64 size table, struct-of-arrays; empty for almost every RF64
	// file, since only a chunk that itself exceeds 4 GiB needs an entry.
	ds64Ids   []uint32
	ds64Sizes []uint64

	format       pcm.Format
	frames       int64
	dataOff      int64
	dataSize     int64
	ds64DataSize int64

	blockAlign     int32
	bytesPerSample int32

	bits      uint16
	validBits uint16

	encoding  EncodingE
	rf64      bool
	haveDs64  bool
	haveFmt   bool
	truncated bool
}

var _ pcm.SourceI = (*File)(nil)

// NewReaderE reads the header of the size-byte stream ra and returns a File
// positioned to decode from its data chunk. ra is retained and not owned:
// CloseE closes nothing that was not opened by [OpenE].
func NewReaderE(ra io.ReaderAt, size int64) (file *File, err error) {
	if ra == nil {
		return nil, eh.New("nil reader")
	}
	file = &File{ra: ra}
	err = file.parseE(size)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// OpenE opens path and reads its header. CloseE closes the file.
func OpenE(path string) (file *File, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eb.Build().Str("path", path).Errorf("open wave file: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, eb.Build().Str("path", path).Errorf("stat wave file: %w", err)
	}
	file, err = NewReaderE(f, st.Size())
	if err != nil {
		_ = f.Close()
		return nil, eb.Build().Str("path", path).Errorf("read wave header: %w", err)
	}
	file.closer = f
	return file, nil
}

// Format implements [pcm.SourceI].
func (inst *File) Format() (format pcm.Format) { return inst.format }

// Frames implements [pcm.SourceI]. It is derived from the data chunk's size
// and the block alignment, clamped to the bytes the stream actually holds.
func (inst *File) Frames() (frames int64) { return inst.frames }

// BitsPerSample is the width of the sample container in the data chunk.
func (inst *File) BitsPerSample() (bits uint16) { return inst.bits }

// ValidBitsPerSample is WAVE_FORMAT_EXTENSIBLE's wValidBitsPerSample, and
// equals BitsPerSample for every other format. A narrower value means the
// sample is left-justified in its container, which does not change the
// conversion scale.
func (inst *File) ValidBitsPerSample() (bits uint16) { return inst.validBits }

// Encoding is the sample layout the data chunk holds, with
// WAVE_FORMAT_EXTENSIBLE already resolved through its SubFormat GUID.
func (inst *File) Encoding() (enc EncodingE) { return inst.encoding }

// IsRF64 reports whether the container was RF64 or BW64 rather than RIFF.
func (inst *File) IsRF64() (yes bool) { return inst.rf64 }

// IsTruncated reports whether the data chunk claimed more bytes than the
// stream holds. The frame count is then what the bytes support, and the tail
// of the recording is simply absent.
func (inst *File) IsTruncated() (yes bool) { return inst.truncated }

// CloseE implements [pcm.SourceI]. It closes the underlying file when this
// File came from [OpenE], and does nothing for a reader handed to
// [NewReaderE].
func (inst *File) CloseE() (err error) {
	if inst.closer == nil {
		return nil
	}
	c := inst.closer
	inst.closer = nil
	err = c.Close()
	if err != nil {
		return eh.Errorf("close wave file: %w", err)
	}
	return nil
}

// ReadFramesAtE implements [pcm.SourceI]: it reads exactly the bytes the
// request needs and converts them to interleaved float32.
func (inst *File) ReadFramesAtE(ctx context.Context, frameOffset int64, dst []float32) (n int, err error) {
	n, err = pcm.ClampReadE(inst.format, inst.frames, frameOffset, dst)
	if err != nil || n == 0 {
		return n, err
	}
	err = ctx.Err()
	if err != nil {
		return 0, err
	}
	ba := int(inst.blockAlign)
	need := n * ba
	if cap(inst.scratch) < need {
		inst.scratch = make([]byte, need)
	}
	buf := inst.scratch[:need]
	_, err = inst.ra.ReadAt(buf, inst.dataOff+frameOffset*int64(ba))
	if err != nil {
		return 0, eb.Build().
			Int64("frameOffset", frameOffset).
			Int("bytes", need).
			Errorf("read samples: %w", err)
	}
	inst.decode(buf, dst[:n*int(inst.format.Channels)])
	return n, nil
}

// decode converts whole frames of raw container bytes to float32. len(src)
// is len(dst) times the sample width by construction.
func (inst *File) decode(src []byte, dst []float32) {
	switch inst.encoding {
	case EncodingPCMInt:
		switch inst.bytesPerSample {
		case 1:
			// 8-bit WAVE samples are unsigned with 128 as silence.
			for i := range dst {
				dst[i] = (float32(src[i]) - 128) / 128
			}
		case 2:
			for i := range dst {
				dst[i] = float32(int16(binary.LittleEndian.Uint16(src[i*2:]))) / 32768
			}
		case 3:
			for i := range dst {
				b := src[i*3 : i*3+3]
				v := int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(int8(b[2]))<<16)
				dst[i] = float32(v) / 8388608
			}
		case 4:
			for i := range dst {
				dst[i] = float32(int32(binary.LittleEndian.Uint32(src[i*4:]))) / 2147483648
			}
		}
	case EncodingIEEEFloat:
		switch inst.bytesPerSample {
		case 4:
			for i := range dst {
				dst[i] = math.Float32frombits(binary.LittleEndian.Uint32(src[i*4:]))
			}
		case 8:
			for i := range dst {
				dst[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(src[i*8:])))
			}
		}
	}
}

// parseE walks the container and fills in everything the read path needs.
func (inst *File) parseE(size int64) (err error) {
	if size < riffHeaderSize {
		return eb.Build().Int64("size", size).Errorf("stream is too short to hold a riff header")
	}
	var buf [40]byte
	_, err = inst.ra.ReadAt(buf[:riffHeaderSize], 0)
	if err != nil {
		return eh.Errorf("read riff header: %w", err)
	}
	riffID := readFourCC(buf[0:4])
	switch riffID {
	case ccRIFF:
	case ccRF64, ccBW64:
		inst.rf64 = true
	default:
		return eb.Build().
			Str("fourCC", fourCCString(riffID)).
			Errorf("leading four-cc %q is not a riff container", fourCCString(riffID))
	}
	formID := readFourCC(buf[8:12])
	if formID != ccWAVE {
		return eb.Build().
			Str("formType", fourCCString(formID)).
			Errorf("riff form type %q is not a wave", fourCCString(formID))
	}

	haveData := false
	pos := riffHeaderSize
	for pos+chunkHeaderSize <= size {
		_, err = inst.ra.ReadAt(buf[:chunkHeaderSize], pos)
		if err != nil {
			return eb.Build().Int64("offset", pos).Errorf("read chunk header: %w", err)
		}
		id := readFourCC(buf[0:4])
		var chunkSize int64
		chunkSize, err = inst.resolveChunkSizeE(id, binary.LittleEndian.Uint32(buf[4:8]))
		if err != nil {
			return err
		}
		body := pos + chunkHeaderSize
		switch id {
		case ccDs64:
			err = inst.checkChunkFitsE(id, body, chunkSize, size)
			if err != nil {
				return err
			}
			err = inst.parseDs64E(body, chunkSize)
		case ccFmt:
			err = inst.checkChunkFitsE(id, body, chunkSize, size)
			if err != nil {
				return err
			}
			err = inst.parseFmtE(body, chunkSize)
		case ccData:
			// A stream with several data chunks is not something this
			// decoder models; the first one is the recording.
			if !haveData {
				inst.dataOff = body
				inst.dataSize = chunkSize
				haveData = true
			}
		}
		if err != nil {
			return err
		}
		advance := chunkSize + (chunkSize & 1)
		if advance > size-body {
			// The chunk runs to or past the end of the stream, so there is
			// nothing after it to walk. A truncated data chunk lands here.
			break
		}
		pos = body + advance
	}

	if !inst.haveFmt {
		return eh.New("stream has no fmt chunk")
	}
	if !haveData {
		return eh.New("stream has no data chunk")
	}
	available := size - inst.dataOff
	if available < 0 {
		available = 0
	}
	if inst.dataSize > available {
		inst.dataSize = available
		inst.truncated = true
	}
	inst.frames = inst.dataSize / int64(inst.blockAlign)
	return nil
}

func (inst *File) checkChunkFitsE(id uint32, body int64, chunkSize int64, size int64) (err error) {
	if body+chunkSize > size {
		return eb.Build().
			Str("chunk", fourCCString(id)).
			Int64("offset", body).
			Int64("chunkSize", chunkSize).
			Int64("size", size).
			Errorf("chunk %q extends past the end of the stream", fourCCString(id))
	}
	return nil
}

// resolveChunkSizeE turns a chunk's 32-bit size field into a real size,
// following the RF64 escape into ds64 where it is set.
func (inst *File) resolveChunkSizeE(id uint32, size32 uint32) (n int64, err error) {
	if !inst.rf64 || size32 != maxUint32 {
		return int64(size32), nil
	}
	if !inst.haveDs64 {
		return 0, eb.Build().
			Str("chunk", fourCCString(id)).
			Errorf("chunk %q escapes to a 64-bit size but no ds64 chunk preceded it", fourCCString(id))
	}
	if id == ccData {
		return inst.ds64DataSize, nil
	}
	for i, cc := range inst.ds64Ids {
		if cc == id {
			return toInt64E("ds64TableSize", inst.ds64Sizes[i])
		}
	}
	return 0, eb.Build().
		Str("chunk", fourCCString(id)).
		Errorf("chunk %q escapes to a 64-bit size but the ds64 table does not list it", fourCCString(id))
}

// parseDs64E reads EBU Tech 3306's ds64 chunk. Of its three 64-bit counts
// only dataSize is load-bearing here: riffSize describes a total this reader
// derives from the stream, and sampleCount is advisory — the frame count
// comes from the bytes that exist.
func (inst *File) parseDs64E(off int64, chunkSize int64) (err error) {
	if chunkSize < ds64BodySize {
		return eb.Build().Int64("chunkSize", chunkSize).Errorf("ds64 chunk is too short")
	}
	var buf [28]byte
	_, err = inst.ra.ReadAt(buf[:], off)
	if err != nil {
		return eh.Errorf("read ds64 chunk: %w", err)
	}
	inst.ds64DataSize, err = toInt64E("dataSize", binary.LittleEndian.Uint64(buf[8:16]))
	if err != nil {
		return err
	}
	tableLen := int64(binary.LittleEndian.Uint32(buf[24:28]))
	if tableLen*ds64TableEntrySize > chunkSize-ds64BodySize {
		return eb.Build().
			Int64("tableLength", tableLen).
			Int64("chunkSize", chunkSize).
			Errorf("ds64 size table does not fit in its chunk")
	}
	inst.ds64Ids = make([]uint32, 0, tableLen)
	inst.ds64Sizes = make([]uint64, 0, tableLen)
	var entry [12]byte
	entryOff := off + ds64BodySize
	for range tableLen {
		_, err = inst.ra.ReadAt(entry[:], entryOff)
		if err != nil {
			return eb.Build().Int64("offset", entryOff).Errorf("read ds64 table entry: %w", err)
		}
		inst.ds64Ids = append(inst.ds64Ids, readFourCC(entry[0:4]))
		inst.ds64Sizes = append(inst.ds64Sizes, binary.LittleEndian.Uint64(entry[4:12]))
		entryOff += ds64TableEntrySize
	}
	inst.haveDs64 = true
	return nil
}

// parseFmtE reads WAVEFORMAT, WAVEFORMATEX or WAVEFORMATEXTENSIBLE, whichever
// the chunk's length says it is.
func (inst *File) parseFmtE(off int64, chunkSize int64) (err error) {
	if chunkSize < fmtChunkSize {
		return eb.Build().Int64("chunkSize", chunkSize).Errorf("fmt chunk is too short")
	}
	var buf [40]byte
	want := chunkSize
	if want > int64(len(buf)) {
		want = int64(len(buf))
	}
	_, err = inst.ra.ReadAt(buf[:want], off)
	if err != nil {
		return eh.Errorf("read fmt chunk: %w", err)
	}
	tag := binary.LittleEndian.Uint16(buf[0:2])
	channels := binary.LittleEndian.Uint16(buf[2:4])
	sampleRate := binary.LittleEndian.Uint32(buf[4:8])
	// buf[8:12] is nAvgBytesPerSec, advisory and derivable, so it is ignored.
	declaredBlockAlign := binary.LittleEndian.Uint16(buf[12:14])
	bits := binary.LittleEndian.Uint16(buf[14:16])
	validBits := bits
	if tag == formatTagExtensible {
		if want < 40 {
			return eb.Build().
				Int64("chunkSize", chunkSize).
				Errorf("extensible fmt chunk is too short to hold a sub-format guid")
		}
		cbSize := binary.LittleEndian.Uint16(buf[16:18])
		if cbSize < 22 {
			return eb.Build().
				Uint16("cbSize", cbSize).
				Errorf("extensible fmt chunk declares %d extension bytes where 22 are needed", cbSize)
		}
		validBits = binary.LittleEndian.Uint16(buf[18:20])
		// buf[20:24] is dwChannelMask, the speaker layout, which nothing
		// downstream reads yet.
		tag, err = subFormatTagE(buf[24:40])
		if err != nil {
			return err
		}
	}
	switch tag {
	case formatTagPCM:
		inst.encoding = EncodingPCMInt
	case formatTagIEEEFloat:
		inst.encoding = EncodingIEEEFloat
	default:
		return eb.Build().
			Uint16("formatTag", tag).
			Errorf("unsupported wave format tag 0x%04x", tag)
	}
	err = validateSampleFormatE(inst.encoding, bits)
	if err != nil {
		return err
	}
	if validBits == 0 || validBits > bits {
		return eb.Build().
			Uint16("validBitsPerSample", validBits).
			Uint16("bitsPerSample", bits).
			Errorf("valid bits per sample does not fit its container")
	}
	format := pcm.Format{SampleRate: sampleRate, Channels: channels}
	err = format.ValidateE()
	if err != nil {
		return err
	}
	blockAlign := int64(channels) * int64(bits/8)
	if blockAlign > int64(^uint16(0)) {
		return eb.Build().
			Int64("blockAlign", blockAlign).
			Errorf("frame of %d bytes is wider than the block-align field", blockAlign)
	}
	if declaredBlockAlign != 0 && int64(declaredBlockAlign) != blockAlign {
		return eb.Build().
			Uint16("declaredBlockAlign", declaredBlockAlign).
			Int64("blockAlign", blockAlign).
			Errorf("declared block align disagrees with channels times sample width")
	}
	inst.format = format
	inst.bits = bits
	inst.validBits = validBits
	inst.bytesPerSample = int32(bits / 8)
	inst.blockAlign = int32(blockAlign)
	inst.haveFmt = true
	return nil
}

func toInt64E(field string, v uint64) (n int64, err error) {
	if v > uint64(math.MaxInt64) {
		return 0, eb.Build().
			Str("field", field).
			Errorf("64-bit size does not fit a signed offset")
	}
	return int64(v), nil
}
