package adhocdata

import (
	"encoding/binary"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// SeekableReader is a random-access reader over a BXAD stream's
// plaintext. It exists so the /table endpoint can honor HTTP range
// requests: ClickHouse's Arrow reader skips column buffers it does not
// need by re-requesting the source from a later offset, and a
// stream-only reader cannot serve that (ADR-0134 update 2026-08-01).
//
// Random access is sound because the format pins the geometry: every
// non-final chunk carries exactly chunk-size plaintext, so a plaintext
// offset maps arithmetically to a chunk index and the chunk's
// ciphertext location. Each chunk authenticates independently under
// its counter nonce, and the total plaintext size falls out of the
// ciphertext size — so a corrupt or truncated file fails construction
// or the first touched chunk, never returns wrong bytes.
//
// A SeekableReader is not safe for concurrent use.
type SeekableReader struct {
	ra        io.ReaderAt
	aead      aeadOpener
	aad       []byte
	chunkSize int64
	// nFull is the number of non-final chunks; finalPlain the final
	// chunk's plaintext length (0..chunkSize). plainSize is the total.
	nFull      int64
	finalPlain int64
	plainSize  int64

	pos     int64
	chunk   int64 // index of the chunk in plain, -1 = none
	plain   []byte
	ctBuf   []byte
	corrupt error // sticky
}

// aeadOpener is the slice of cipher.AEAD this reader needs; narrowed so
// tests can fault-inject without a second key path.
type aeadOpener interface {
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}

// NewSeekableReader opens a BXAD stream held in ra (size ciphertext
// bytes) for random access under key. The header is read and the
// geometry validated eagerly; chunks authenticate lazily as they are
// first touched.
func NewSeekableReader(ra io.ReaderAt, size int64, key []byte) (inst *SeekableReader, err error) {
	if len(key) != KeySize {
		err = eh.Errorf("adhocdata: key must be %d bytes, got %d", KeySize, len(key))
		return
	}
	aead, err := newGCM(key)
	if err != nil {
		return
	}
	chunkSize, aad, err := readHeader(io.NewSectionReader(ra, 0, int64(headerSize)))
	if err != nil {
		return
	}
	cs := int64(chunkSize)
	fullCt := lenPrefixSize + cs + tagSize // one non-final chunk on disk
	minCt := int64(lenPrefixSize + tagSize)
	t := size - int64(headerSize)
	if t < minCt {
		err = eh.Errorf("adhocdata: truncated stream: missing final chunk")
		return
	}
	nFull := (t - minCt) / fullCt
	finalPlain := t - nFull*fullCt - minCt
	if finalPlain < 0 || finalPlain > cs {
		err = eh.Errorf("adhocdata: ciphertext size %d does not fit the chunk geometry", size)
		return
	}
	inst = &SeekableReader{
		ra:         ra,
		aead:       aead,
		aad:        aad,
		chunkSize:  cs,
		nFull:      nFull,
		finalPlain: finalPlain,
		plainSize:  nFull*cs + finalPlain,
		chunk:      -1,
	}
	return
}

// PlaintextSize reports the stream's total plaintext length.
func (inst *SeekableReader) PlaintextSize() int64 { return inst.plainSize }

// Seek implements io.Seeker over the plaintext. Seeking past the end is
// legal (reads then return io.EOF), before the start is an error.
func (inst *SeekableReader) Seek(offset int64, whence int) (pos int64, err error) {
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = inst.pos + offset
	case io.SeekEnd:
		pos = inst.plainSize + offset
	default:
		err = eh.Errorf("adhocdata: seek: invalid whence %d", whence)
		return
	}
	if pos < 0 {
		err = eh.Errorf("adhocdata: seek: negative position %d", pos)
		pos = inst.pos
		return
	}
	inst.pos = pos
	return
}

// Read implements io.Reader at the current position. Reads never span a
// chunk boundary in one call; callers loop (io.Copy and friends do).
func (inst *SeekableReader) Read(p []byte) (n int, err error) {
	if inst.corrupt != nil {
		return 0, inst.corrupt
	}
	if inst.pos >= inst.plainSize {
		return 0, io.EOF
	}
	idx := inst.pos / inst.chunkSize
	if idx != inst.chunk {
		if err = inst.load(idx); err != nil {
			inst.corrupt = err
			return 0, err
		}
	}
	off := inst.pos - idx*inst.chunkSize
	n = copy(p, inst.plain[off:])
	inst.pos += int64(n)
	return
}

// load decrypts chunk idx into inst.plain. The expected ciphertext
// length is fully determined by the geometry, so any mismatch —
// including a tampered length prefix — is rejected before the AEAD
// even runs; the AEAD then authenticates the bytes themselves.
func (inst *SeekableReader) load(idx int64) (err error) {
	final := idx == inst.nFull
	wantPlain := inst.chunkSize
	if final {
		wantPlain = inst.finalPlain
	}
	diskOff := int64(headerSize) + idx*(lenPrefixSize+inst.chunkSize+tagSize)
	var lenBuf [lenPrefixSize]byte
	if _, err = inst.ra.ReadAt(lenBuf[:], diskOff); err != nil {
		return eh.Errorf("adhocdata: read chunk length: %w", err)
	}
	ctLen := int64(binary.LittleEndian.Uint32(lenBuf[:]))
	if ctLen != wantPlain+tagSize {
		return eh.Errorf("adhocdata: chunk %d length %d, geometry wants %d", idx, ctLen, wantPlain+tagSize)
	}
	if int64(cap(inst.ctBuf)) < ctLen {
		inst.ctBuf = make([]byte, ctLen)
	}
	ct := inst.ctBuf[:ctLen]
	if _, err = inst.ra.ReadAt(ct, diskOff+lenPrefixSize); err != nil {
		return eh.Errorf("adhocdata: read chunk %d: %w", idx, err)
	}
	nonce := makeNonce(uint64(idx), final)
	plain, err := inst.aead.Open(inst.plain[:0], nonce[:], ct, inst.aad)
	if err != nil {
		return eh.Errorf("adhocdata: authenticate chunk %d: %w", idx, err)
	}
	inst.plain = plain
	inst.chunk = idx
	return
}
