// Package sealed holds bytes that must not outlive the process that wrote
// them (ADR-0240 §SD1). A [File] is an unnamed inode under the base
// directory — created with O_TMPFILE, so it has no directory entry, cannot
// acquire one, and is freed by the kernel on last close, crash included —
// whose contents are chunk-encrypted under a key that exists only inside
// the File. Whoever holds the *File can read it; nobody can look a key up,
// because no such operation exists. The two clients in tree are the ad-hoc
// dataset capability and tally's recording stage.
//
// This file carries the on-disk format, a segmented AEAD stream in the
// STREAM construction: it authenticates incrementally at constant memory
// and detects truncation, and its fixed geometry is what makes random
// access a matter of arithmetic.
//
// Format (little-endian lengths, big-endian nonce fields):
//
//	header  = magic "BXAD" (4) | version u8 (1) | chunk-size u32 (4)   // 9 bytes
//	chunk   = ct-len u32 (4) | ciphertext (ct-len, plaintext+GCM tag)
//	stream  = header chunk*                                            // ≥1 chunk
//
// Each chunk is sealed with AES-256-GCM under a 12-byte nonce built as an
// 8-byte big-endian chunk counter followed by a 4-byte flags word whose
// bit 0 marks the final chunk. The header bytes are the AAD, so the version
// and chunk size are authenticated on every chunk. A non-final chunk always
// carries exactly chunk-size plaintext; the final chunk carries
// 0..chunk-size and is always present (an empty stream is a single empty
// final chunk). Because finality is bound into the nonce, truncating the
// stream — dropping the final chunk, or cutting a chunk short — makes the
// last readable chunk fail authentication, so no truncated prefix is ever
// accepted as complete.
package sealed

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"io"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// KeySize is the AES-256 key length in bytes.
	KeySize = 32
	// ChunkSize is the plaintext bytes per non-final chunk. 64 KiB keeps
	// per-chunk overhead negligible while bounding a reader's working set.
	ChunkSize = 64 * 1024

	magic         = "BXAD"
	formatVersion = 1
	headerSize    = len(magic) + 1 + 4 // magic | version | chunk-size
	nonceSize     = 12
	tagSize       = 16 // AES-GCM authentication tag
	lenPrefixSize = 4

	// maxChunkSize bounds the chunk size a reader will honour from a
	// header, so a corrupt or hostile header cannot force a huge
	// allocation. Well above ChunkSize to leave room for a future widening
	// without a format break.
	maxChunkSize = 1 << 26 // 64 MiB
)

// Writer seals a plaintext stream into the chunk format. Write emits full
// non-final chunks as they accumulate and holds back at most one chunk —
// the one that may still turn out to be final — so a write of any size
// costs one pass over its bytes. Close seals the trailing bytes as the
// final chunk and MUST be called to produce a valid stream. A Writer is
// not safe for concurrent use.
type Writer struct {
	w         io.Writer
	aead      cipher.AEAD
	aad       []byte
	chunkSize int
	counter   uint64
	buf       []byte // pending plaintext, ≤ chunkSize after every Write
	ctBuf     []byte // reused ciphertext scratch
	lenBuf    [lenPrefixSize]byte
	written   int64 // ciphertext bytes emitted, header included
	plain     int64 // plaintext bytes accepted
	closed    bool
	err       error // sticky
	onClose   func(ciphertext, plaintext int64)
}

// newWriter writes the header to w and returns a Writer sealing under key
// with the given chunk size. Tests pass small chunk sizes to exercise the
// many-chunk paths cheaply; the reader recovers the size from the header.
func newWriter(w io.Writer, key []byte, chunkSize int) (inst *Writer, err error) {
	if len(key) != KeySize {
		err = eb.Build().Int("want", KeySize).Int("got", len(key)).Errorf("sealed: key has the wrong length")
		return
	}
	if chunkSize <= 0 || chunkSize > maxChunkSize {
		err = eb.Build().Int("chunkSize", chunkSize).Int("max", maxChunkSize).Errorf("sealed: chunk size out of range")
		return
	}
	aead, err := newGCM(key)
	if err != nil {
		return
	}
	aad := makeHeader(chunkSize)
	if _, err = w.Write(aad); err != nil {
		err = eh.Errorf("sealed: write header: %w", err)
		return
	}
	inst = &Writer{
		w:         w,
		aead:      aead,
		aad:       aad,
		chunkSize: chunkSize,
		buf:       make([]byte, 0, chunkSize),
		ctBuf:     make([]byte, 0, chunkSize+tagSize),
		written:   int64(len(aad)),
	}
	return
}

// Write seals p. Whole chunks are sealed straight out of p while more
// input follows them; only a remainder of at most one chunk is copied
// into the pending buffer, because whether it is final is Close's to say.
func (inst *Writer) Write(p []byte) (n int, err error) {
	if inst.err != nil {
		return 0, inst.err
	}
	if inst.closed {
		return 0, eh.Errorf("sealed: write after close")
	}
	n = len(p)
	inst.plain += int64(n)
	cs := inst.chunkSize
	for len(p) > 0 {
		if len(inst.buf) == 0 && len(p) > cs {
			// A full chunk with input still behind it cannot be final.
			if err = inst.sealChunk(p[:cs], false); err != nil {
				inst.err = err
				return 0, err
			}
			p = p[cs:]
			continue
		}
		take := copy(inst.buf[len(inst.buf):cs], p)
		inst.buf = inst.buf[:len(inst.buf)+take]
		p = p[take:]
		if len(p) == 0 {
			break
		}
		// The buffer is full and more input follows: not final either.
		if err = inst.sealChunk(inst.buf, false); err != nil {
			inst.err = err
			return 0, err
		}
		inst.buf = inst.buf[:0]
	}
	return n, nil
}

// Close seals the pending remainder (0..chunk-size bytes) as the final
// chunk. It is idempotent; a second call returns the first result.
func (inst *Writer) Close() (err error) {
	if inst.err != nil {
		return inst.err
	}
	if inst.closed {
		return nil
	}
	inst.closed = true
	if err = inst.sealChunk(inst.buf, true); err != nil {
		inst.err = err
		return
	}
	inst.buf = nil
	if inst.onClose != nil {
		inst.onClose(inst.written, inst.plain)
	}
	return
}

// sealChunk encrypts plain as chunk number counter and writes its
// length-prefixed ciphertext. plain must be ≤ chunkSize; for a non-final
// chunk it must equal chunkSize (the reader enforces the same).
func (inst *Writer) sealChunk(plain []byte, final bool) (err error) {
	nonce := makeNonce(inst.counter, final)
	inst.counter++
	ct := inst.aead.Seal(inst.ctBuf[:0], nonce[:], plain, inst.aad)
	binary.LittleEndian.PutUint32(inst.lenBuf[:], uint32(len(ct)))
	if _, err = inst.w.Write(inst.lenBuf[:]); err != nil {
		return eh.Errorf("sealed: write chunk length: %w", err)
	}
	if _, err = inst.w.Write(ct); err != nil {
		return eh.Errorf("sealed: write chunk: %w", err)
	}
	inst.written += int64(lenPrefixSize + len(ct))
	return
}

// Reader is random access over a sealed stream's plaintext: an
// io.ReadSeeker for streaming consumers and an io.ReaderAt for positioned
// ones, one cached chunk between them. Random access is sound because the
// format pins the geometry: every non-final chunk carries exactly
// chunk-size plaintext, so a plaintext offset maps arithmetically to a
// chunk and its ciphertext location; each chunk authenticates
// independently under its counter nonce; and the total plaintext size
// falls out of the ciphertext size. A corrupt or truncated stream fails
// construction or the first touched chunk, never returns wrong bytes, and
// the failure is sticky.
//
// A Reader is safe for concurrent use; the cost is one mutex around a
// chunk decrypt, which is the granularity at which concurrent readers of
// one recording contend anyway. Close releases it (for a [File], it
// decrements the open-reader count).
type Reader struct {
	ra        io.ReaderAt
	aead      aeadOpener
	aad       []byte
	chunkSize int64
	// nFull is the number of non-final chunks, finalPlain the final
	// chunk's plaintext length (0..chunkSize), plainSize the total.
	nFull      int64
	finalPlain int64
	plainSize  int64

	mu      sync.Mutex
	pos     int64
	chunk   int64 // index of the chunk in plain, -1 = none
	plain   []byte
	ctBuf   []byte
	corrupt error // sticky
	onClose func()
	closed  bool
}

// aeadOpener is the slice of cipher.AEAD the reader needs; narrowed so
// tests can fault-inject without a second key path.
type aeadOpener interface {
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}

// newReader opens a sealed stream held in ra (size ciphertext bytes)
// under key. The header is read and the geometry validated eagerly; chunks
// authenticate lazily as they are first touched.
func newReader(ra io.ReaderAt, size int64, key []byte) (inst *Reader, err error) {
	if len(key) != KeySize {
		err = eb.Build().Int("want", KeySize).Int("got", len(key)).Errorf("sealed: key has the wrong length")
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
		err = eh.Errorf("sealed: truncated stream: missing final chunk")
		return
	}
	nFull := (t - minCt) / fullCt
	finalPlain := t - nFull*fullCt - minCt
	if finalPlain < 0 || finalPlain > cs {
		err = eb.Build().Int64("size", size).Errorf("sealed: ciphertext size does not fit the chunk geometry")
		return
	}
	inst = &Reader{
		ra:         ra,
		aead:       aead,
		aad:        aad,
		chunkSize:  cs,
		nFull:      nFull,
		finalPlain: finalPlain,
		plainSize:  nFull*cs + finalPlain,
		chunk:      -1,
	}
	// The geometry is a claim the ciphertext size makes about itself; the
	// final chunk's nonce is what proves it. Authenticating that chunk now
	// rejects every truncation the arithmetic alone cannot see — a prefix
	// ending on a full chunk reads as a stream whose last chunk is final,
	// and a prefix of header plus one empty chunk reads as an empty stream
	// no read would ever touch.
	if err = inst.load(nFull); err != nil {
		return nil, err
	}
	return
}

// PlaintextSize reports the stream's total plaintext length.
func (inst *Reader) PlaintextSize() (n int64) { return inst.plainSize }

// Seek implements io.Seeker over the plaintext. Seeking past the end is
// legal (reads then return io.EOF); before the start is an error.
func (inst *Reader) Seek(offset int64, whence int) (pos int64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = inst.pos + offset
	case io.SeekEnd:
		pos = inst.plainSize + offset
	default:
		err = eb.Build().Int("whence", whence).Errorf("sealed: seek: invalid whence")
		return
	}
	if pos < 0 {
		err = eb.Build().Int64("pos", pos).Errorf("sealed: seek: negative position")
		pos = inst.pos
		return
	}
	inst.pos = pos
	return
}

// Read implements io.Reader at the current position. A read never spans a
// chunk boundary in one call; callers loop (io.Copy and friends do).
func (inst *Reader) Read(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n, err = inst.readAtLocked(p, inst.pos)
	inst.pos += int64(n)
	return
}

// ReadAt implements io.ReaderAt over the plaintext. It fills p across
// chunk boundaries, as the contract requires, and reports io.EOF only
// when the plaintext ends before p is full.
func (inst *Reader) ReadAt(p []byte, off int64) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for n < len(p) {
		var m int
		m, err = inst.readAtLocked(p[n:], off+int64(n))
		n += m
		if err != nil {
			return
		}
	}
	return
}

// readAtLocked reads from one chunk at off; it is the primitive Read and
// ReadAt share.
func (inst *Reader) readAtLocked(p []byte, off int64) (n int, err error) {
	if inst.corrupt != nil {
		return 0, inst.corrupt
	}
	if inst.closed {
		return 0, eh.Errorf("sealed: read after close")
	}
	if off < 0 {
		return 0, eb.Build().Int64("off", off).Errorf("sealed: negative offset")
	}
	if off >= inst.plainSize {
		return 0, io.EOF
	}
	idx := off / inst.chunkSize
	if idx != inst.chunk {
		if err = inst.load(idx); err != nil {
			inst.corrupt = err
			return 0, err
		}
	}
	n = copy(p, inst.plain[off-idx*inst.chunkSize:])
	return
}

// load decrypts chunk idx into inst.plain. The expected ciphertext length
// is fully determined by the geometry, so any mismatch — including a
// tampered length prefix — is rejected before the AEAD runs; the AEAD then
// authenticates the bytes themselves.
func (inst *Reader) load(idx int64) (err error) {
	final := idx == inst.nFull
	wantPlain := inst.chunkSize
	if final {
		wantPlain = inst.finalPlain
	}
	diskOff := int64(headerSize) + idx*(lenPrefixSize+inst.chunkSize+tagSize)
	var lenBuf [lenPrefixSize]byte
	if _, err = inst.ra.ReadAt(lenBuf[:], diskOff); err != nil {
		return eh.Errorf("sealed: read chunk length: %w", err)
	}
	ctLen := int64(binary.LittleEndian.Uint32(lenBuf[:]))
	if ctLen != wantPlain+tagSize {
		return eb.Build().Int64("chunk", idx).Int64("length", ctLen).Int64("want", wantPlain+tagSize).Errorf("sealed: chunk length does not match the geometry")
	}
	if int64(cap(inst.ctBuf)) < ctLen {
		inst.ctBuf = make([]byte, ctLen)
	}
	ct := inst.ctBuf[:ctLen]
	if _, err = inst.ra.ReadAt(ct, diskOff+lenPrefixSize); err != nil {
		return eb.Build().Int64("idx", idx).Errorf("sealed: read chunk: %w", err)
	}
	nonce := makeNonce(uint64(idx), final)
	plain, err := inst.aead.Open(inst.plain[:0], nonce[:], ct, inst.aad)
	if err != nil {
		return eb.Build().Int64("idx", idx).Errorf("sealed: authenticate chunk: %w", err)
	}
	inst.plain = plain
	inst.chunk = idx
	return
}

// Close releases the reader. Idempotent.
func (inst *Reader) Close() (err error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return
	}
	inst.closed = true
	inst.plain = nil
	onClose := inst.onClose
	inst.mu.Unlock()
	if onClose != nil {
		onClose()
	}
	return
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key.
func newGCM(key []byte) (aead cipher.AEAD, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, eh.Errorf("sealed: new cipher: %w", err)
	}
	aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, eh.Errorf("sealed: new gcm: %w", err)
	}
	return
}

// makeHeader renders the 9-byte header, which is also the per-chunk AAD.
func makeHeader(chunkSize int) (hdr []byte) {
	hdr = make([]byte, headerSize)
	copy(hdr[0:len(magic)], magic)
	hdr[len(magic)] = formatVersion
	binary.LittleEndian.PutUint32(hdr[len(magic)+1:], uint32(chunkSize))
	return
}

// readHeader consumes and validates the header, returning the declared
// chunk size and the header bytes (the AAD).
func readHeader(r io.Reader) (chunkSize int, aad []byte, err error) {
	hdr := make([]byte, headerSize)
	if _, err = io.ReadFull(r, hdr); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			err = eh.Errorf("sealed: truncated stream: short header")
		} else {
			err = eh.Errorf("sealed: read header: %w", err)
		}
		return
	}
	if string(hdr[0:len(magic)]) != magic {
		err = eh.Errorf("sealed: bad magic")
		return
	}
	if hdr[len(magic)] != formatVersion {
		err = eb.Build().Uint8("version", hdr[len(magic)]).Errorf("sealed: unsupported format version")
		return
	}
	cs := binary.LittleEndian.Uint32(hdr[len(magic)+1:])
	if cs == 0 || cs > maxChunkSize {
		err = eb.Build().Uint32("chunkSize", cs).Errorf("sealed: chunk size out of range")
		return
	}
	chunkSize = int(cs)
	aad = hdr
	return
}

// makeNonce builds the 12-byte nonce for chunk counter: an 8-byte
// big-endian counter and a 4-byte flags word (bit 0 = final chunk).
func makeNonce(counter uint64, final bool) (nonce [nonceSize]byte) {
	binary.BigEndian.PutUint64(nonce[0:8], counter)
	var flags uint32
	if final {
		flags = 1
	}
	binary.BigEndian.PutUint32(nonce[8:12], flags)
	return
}
