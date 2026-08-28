package peaks

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

const (
	// CacheMagic opens every peaks file.
	CacheMagic = "BXPK"
	// CacheVersion is the on-disk format version; a reader refuses anything
	// else rather than guessing.
	CacheVersion uint32 = 1
	// headerBytes is the fixed header size. Fields are little-endian and
	// laid out so every scalar is naturally aligned:
	//
	//	 0 magic[4] | 4 version u32 | 8 identity hash[32]
	//	40 identity size i64 | 48 identity mtime i64
	//	56 sample rate u32 | 60 channels u16 | 62 reserved u16
	//	64 frames i64 | 72 base bin i32 | 76 levels i32
	headerBytes = 80
	// ioChunkBytes is the staging size for the int8 <-> byte conversion of
	// the level arrays.
	ioChunkBytes = 1 << 16
)

// Identity names the source a peaks file was built from. This package does
// not compute it — ADR-0208 §SD4 keys the cache on a hash of the file's
// size, modification time and head/tail bytes, which is the caller's
// business — it stores the value verbatim and compares it on load, so a
// cache that belongs to another file is rejected instead of drawn.
type Identity struct {
	Hash            [32]byte
	SizeBytes       int64
	ModTimeUnixNano int64
}

// WriteToE serialises a complete pyramid: the header, then the level arrays
// verbatim (ADR-0208 §SD12 — peaks and nothing else). An incomplete
// pyramid, or one finished short of its declared frame count, is refused.
func (inst *Pyramid) WriteToE(w io.Writer, id Identity) (err error) {
	built := inst.built.Load()
	if !inst.complete.Load() || built != inst.frames {
		return eb.Build().
			Bool("complete", inst.complete.Load()).
			Int64("built", built).
			Int64("frames", inst.frames).
			Errorf("refusing to write an incomplete pyramid")
	}

	var hdr [headerBytes]byte
	copy(hdr[0:4], CacheMagic)
	binary.LittleEndian.PutUint32(hdr[4:8], CacheVersion)
	copy(hdr[8:40], id.Hash[:])
	binary.LittleEndian.PutUint64(hdr[40:48], uint64(id.SizeBytes))
	binary.LittleEndian.PutUint64(hdr[48:56], uint64(id.ModTimeUnixNano))
	binary.LittleEndian.PutUint32(hdr[56:60], inst.format.SampleRate)
	binary.LittleEndian.PutUint16(hdr[60:62], inst.format.Channels)
	binary.LittleEndian.PutUint64(hdr[64:72], uint64(inst.frames))
	binary.LittleEndian.PutUint32(hdr[72:76], uint32(inst.baseBin))
	binary.LittleEndian.PutUint32(hdr[76:80], uint32(inst.levels))
	_, err = w.Write(hdr[:])
	if err != nil {
		return eh.Errorf("unable to write peaks header: %w", err)
	}

	body := inst.backing
	scratch := make([]byte, min(len(body), ioChunkBytes))
	for off := 0; off < len(body); off += len(scratch) {
		end := min(off+len(scratch), len(body))
		chunk := body[off:end]
		for i, v := range chunk {
			scratch[i] = byte(v)
		}
		_, err = w.Write(scratch[:len(chunk)])
		if err != nil {
			return eb.Build().Int("offset", off).Errorf("unable to write peaks body: %w", err)
		}
	}
	return nil
}

// ReadFromE reads a peaks file and returns the complete pyramid it holds.
// The magic, the version and every [Identity] field must match want; a
// mismatch is an error naming the field that differs.
func ReadFromE(r io.Reader, want Identity) (inst *Pyramid, err error) {
	var hdr [headerBytes]byte
	_, err = io.ReadFull(r, hdr[:])
	if err != nil {
		return nil, eh.Errorf("unable to read peaks header: %w", err)
	}
	if string(hdr[0:4]) != CacheMagic {
		return nil, eb.Build().
			Str("magic", hex.EncodeToString(hdr[0:4])).
			Str("want", CacheMagic).
			Errorf("not a peaks file")
	}
	version := binary.LittleEndian.Uint32(hdr[4:8])
	if version != CacheVersion {
		return nil, eb.Build().
			Str("field", "version").
			Uint32("got", version).
			Uint32("want", CacheVersion).
			Errorf("unsupported peaks file version")
	}
	if !bytes.Equal(hdr[8:40], want.Hash[:]) {
		return nil, eb.Build().
			Str("field", "hash").
			Str("got", hex.EncodeToString(hdr[8:40])).
			Str("want", hex.EncodeToString(want.Hash[:])).
			Errorf("cache identity mismatch on the source hash")
	}
	sizeBytes := int64(binary.LittleEndian.Uint64(hdr[40:48]))
	if sizeBytes != want.SizeBytes {
		return nil, eb.Build().
			Str("field", "sizeBytes").
			Int64("got", sizeBytes).
			Int64("want", want.SizeBytes).
			Errorf("cache identity mismatch on the source size")
	}
	modTime := int64(binary.LittleEndian.Uint64(hdr[48:56]))
	if modTime != want.ModTimeUnixNano {
		return nil, eb.Build().
			Str("field", "modTimeUnixNano").
			Int64("got", modTime).
			Int64("want", want.ModTimeUnixNano).
			Errorf("cache identity mismatch on the source modification time")
	}
	if reserved := binary.LittleEndian.Uint16(hdr[62:64]); reserved != 0 {
		return nil, eb.Build().Uint16("reserved", reserved).Errorf("reserved header field is not zero")
	}

	format := pcm.Format{
		SampleRate: binary.LittleEndian.Uint32(hdr[56:60]),
		Channels:   binary.LittleEndian.Uint16(hdr[60:62]),
	}
	frames := int64(binary.LittleEndian.Uint64(hdr[64:72]))
	baseBin := int32(binary.LittleEndian.Uint32(hdr[72:76]))
	levels := int32(binary.LittleEndian.Uint32(hdr[76:80]))
	inst, err = NewPyramidE(format, frames, baseBin)
	if err != nil {
		return nil, eh.Errorf("peaks header does not describe a pyramid: %w", err)
	}
	if levels != inst.levels {
		return nil, eb.Build().
			Str("field", "levels").
			Int32("got", levels).
			Int32("want", inst.levels).
			Errorf("level count disagrees with the frame count and base bin")
	}

	body := inst.backing
	scratch := make([]byte, min(len(body), ioChunkBytes))
	for off := 0; off < len(body); off += len(scratch) {
		end := min(off+len(scratch), len(body))
		_, err = io.ReadFull(r, scratch[:end-off])
		if err != nil {
			return nil, eb.Build().
				Int("offset", off).
				Int("bodyBytes", len(body)).
				Errorf("unable to read peaks body: %w", err)
		}
		for i, b := range scratch[:end-off] {
			// -127 is full scale; -128 cannot come out of the quantiser and
			// would break the containment guarantee downstream.
			if b == 0x80 {
				return nil, eb.Build().Int("offset", off+i).Errorf("peak value out of range")
			}
			body[off+i] = int8(b)
		}
	}

	copy(inst.storedBins, inst.binCounts)
	inst.folded = frames
	inst.peak = inst.computePeak()
	inst.globalPeak.Store(int32(inst.peak))
	inst.built.Store(frames)
	inst.complete.Store(true)
	return inst, nil
}

// computePeak derives the global peak from the top level, whose bins
// summarise the whole signal.
func (inst *Pyramid) computePeak() (peak int8) {
	level := inst.levels - 1
	channels := int(inst.format.Channels)
	row := int(level) * channels
	for c := range channels {
		mins := inst.mins[row+c]
		maxs := inst.maxs[row+c]
		for b := range inst.storedBins[level] {
			if p := absPeak(mins[b]); p > peak {
				peak = p
			}
			if p := absPeak(maxs[b]); p > peak {
				peak = p
			}
		}
	}
	return peak
}
