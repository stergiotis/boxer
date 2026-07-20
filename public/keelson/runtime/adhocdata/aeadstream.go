// Package adhocdata implements the ad-hoc dataset store of ADR-0134:
// ephemeral tabular data an app publishes for a SQL applet to query,
// held as chunk-encrypted Arrow files whose keys live only in process
// memory. This file carries the on-disk format — a segmented-AEAD
// stream (the STREAM construction) that authenticates incrementally at
// constant memory and detects truncation, so a crash leaves ciphertext
// whose key no longer exists rather than readable data.
//
// Format (little-endian lengths, big-endian nonce fields):
//
//	header  = magic "BXAD" (4) | version u8 (1) | chunk-size u32 (4)   // 9 bytes
//	chunk   = ct-len u32 (4) | ciphertext (ct-len, plaintext+GCM tag)
//	stream  = header chunk*                                            // ≥1 chunk
//
// Each chunk is sealed with AES-256-GCM under a 12-byte nonce built as
// an 8-byte big-endian chunk counter followed by a 4-byte flags word
// whose bit 0 marks the final chunk. The header bytes are the AAD, so
// the version and chunk size are authenticated on every chunk. A
// non-final chunk always carries exactly chunk-size plaintext; the
// final chunk carries 0..chunk-size and is always present (an empty
// dataset is a single empty final chunk). Because finality is bound
// into the nonce, truncating the stream — dropping the final chunk, or
// cutting a chunk short — makes the last readable chunk fail
// authentication, so no truncated prefix is ever accepted as complete.
package adhocdata

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh"
)

const (
	// KeySize is the AES-256 key length in bytes.
	KeySize = 32
	// ChunkSize is the plaintext bytes per non-final chunk (ADR-0134
	// SD1). 64 KiB keeps per-chunk overhead negligible while bounding
	// the reader's working set.
	ChunkSize = 64 * 1024

	magic         = "BXAD"
	formatVersion = 1
	headerSize    = len(magic) + 1 + 4 // magic | version | chunk-size
	nonceSize     = 12
	tagSize       = 16 // AES-GCM authentication tag
	lenPrefixSize = 4

	// maxChunkSize bounds the chunk size a reader will honour from a
	// file header, so a corrupt or hostile header cannot force a huge
	// allocation. Well above ChunkSize to leave room for a future
	// widening without a format break.
	maxChunkSize = 1 << 26 // 64 MiB
)

// Writer encrypts a plaintext stream into the BXAD chunk format. It
// implements io.WriteCloser: Write buffers plaintext and emits full
// non-final chunks as they accumulate, and Close seals the trailing
// bytes as the final chunk. Close MUST be called to produce a valid
// stream. A Writer is not safe for concurrent use.
type Writer struct {
	w         io.Writer
	aead      cipher.AEAD
	aad       []byte
	chunkSize int
	counter   uint64
	buf       []byte // pending plaintext, always ≤ chunkSize after Write
	ctBuf     []byte // reused ciphertext scratch
	lenBuf    [lenPrefixSize]byte
	closed    bool
	err       error // sticky
}

// NewWriter returns a Writer that encrypts to w under key (32 bytes)
// using the default ChunkSize. It writes the header immediately, so a
// failure to write the header surfaces here.
func NewWriter(w io.Writer, key []byte) (inst *Writer, err error) {
	return newWriterChunk(w, key, ChunkSize)
}

// newWriterChunk is NewWriter with an explicit chunk size, for tests
// that exercise the many-chunk paths without large payloads. The
// reader recovers the chunk size from the header, so files written
// with any chunk size read back correctly.
func newWriterChunk(w io.Writer, key []byte, chunkSize int) (inst *Writer, err error) {
	if len(key) != KeySize {
		err = eh.Errorf("adhocdata: key must be %d bytes, got %d", KeySize, len(key))
		return
	}
	if chunkSize <= 0 || chunkSize > maxChunkSize {
		err = eh.Errorf("adhocdata: chunk size %d out of range (1..%d)", chunkSize, maxChunkSize)
		return
	}
	aead, err := newGCM(key)
	if err != nil {
		return
	}
	aad := makeHeader(chunkSize)
	if _, err = w.Write(aad); err != nil {
		err = eh.Errorf("adhocdata: write header: %w", err)
		return
	}
	inst = &Writer{
		w:         w,
		aead:      aead,
		aad:       aad,
		chunkSize: chunkSize,
		buf:       make([]byte, 0, chunkSize),
		ctBuf:     make([]byte, 0, chunkSize+tagSize),
	}
	return
}

// Write buffers p and flushes complete non-final chunks. A chunk is
// held back once it reaches exactly chunk-size, because it may still
// turn out to be the final chunk — only Close decides that.
func (inst *Writer) Write(p []byte) (n int, err error) {
	if inst.err != nil {
		return 0, inst.err
	}
	if inst.closed {
		return 0, eh.Errorf("adhocdata: write after close")
	}
	n = len(p)
	inst.buf = append(inst.buf, p...)
	for len(inst.buf) > inst.chunkSize {
		if err = inst.sealChunk(inst.buf[:inst.chunkSize], false); err != nil {
			inst.err = err
			return 0, err
		}
		// Compact the remainder to the front; copy handles the
		// overlapping regions like memmove.
		rem := copy(inst.buf, inst.buf[inst.chunkSize:])
		inst.buf = inst.buf[:rem]
	}
	return n, nil
}

// Close seals the buffered remainder (0..chunk-size bytes) as the final
// chunk and flushes it. It is idempotent; a second call returns the
// first result. After Close the stream is complete.
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
		return eh.Errorf("adhocdata: write chunk length: %w", err)
	}
	if _, err = inst.w.Write(ct); err != nil {
		return eh.Errorf("adhocdata: write chunk: %w", err)
	}
	return
}

// Reader decrypts a BXAD stream produced by Writer. It implements
// io.Reader; a truncated stream, a wrong key, or any tampering surfaces
// as an error rather than a short or silently-wrong read. A Reader is
// not safe for concurrent use.
type Reader struct {
	br        *bufio.Reader
	aead      cipher.AEAD
	aad       []byte
	chunkSize int
	counter   uint64
	plain     []byte // decrypted, not yet consumed
	off       int
	done      bool  // final chunk consumed
	err       error // sticky
}

// NewReader returns a Reader over r, decrypting under key (32 bytes).
// It reads and authenticates the header eagerly.
func NewReader(r io.Reader, key []byte) (inst *Reader, err error) {
	if len(key) != KeySize {
		err = eh.Errorf("adhocdata: key must be %d bytes, got %d", KeySize, len(key))
		return
	}
	aead, err := newGCM(key)
	if err != nil {
		return
	}
	br := bufio.NewReader(r)
	chunkSize, aad, err := readHeader(br)
	if err != nil {
		return
	}
	inst = &Reader{
		br:        br,
		aead:      aead,
		aad:       aad,
		chunkSize: chunkSize,
	}
	return
}

// Read delivers decrypted plaintext. It returns io.EOF only after the
// authenticated final chunk has been fully consumed.
func (inst *Reader) Read(p []byte) (n int, err error) {
	if inst.off >= len(inst.plain) {
		if inst.err != nil {
			return 0, inst.err
		}
		if inst.done {
			return 0, io.EOF
		}
		if err = inst.fill(); err != nil {
			inst.err = err
			return 0, err
		}
	}
	n = copy(p, inst.plain[inst.off:])
	inst.off += n
	return
}

// fill decrypts the next chunk into inst.plain. It decides finality by
// peeking one byte past the chunk: EOF means this was the final chunk,
// so it is opened with the final-flag nonce. A stream that ends without
// a final chunk therefore fails authentication here.
func (inst *Reader) fill() (err error) {
	var lenBuf [lenPrefixSize]byte
	_, err = io.ReadFull(inst.br, lenBuf[:])
	if err != nil {
		if err == io.EOF {
			// No chunk where one was required: either a header-only
			// file (a valid stream always has ≥1 final chunk) or a
			// non-final chunk was the last thing on the stream.
			return eh.Errorf("adhocdata: truncated stream: missing final chunk")
		}
		return eh.Errorf("adhocdata: read chunk length: %w", err)
	}
	ctLen := binary.LittleEndian.Uint32(lenBuf[:])
	if ctLen < tagSize || int(ctLen) > inst.chunkSize+tagSize {
		return eh.Errorf("adhocdata: chunk length %d out of range", ctLen)
	}
	ct := make([]byte, ctLen)
	if _, err = io.ReadFull(inst.br, ct); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return eh.Errorf("adhocdata: truncated stream: short chunk")
		}
		return eh.Errorf("adhocdata: read chunk: %w", err)
	}

	final, err := inst.atEOF()
	if err != nil {
		return
	}
	nonce := makeNonce(inst.counter, final)
	inst.counter++
	plain, err := inst.aead.Open(inst.plain[:0], nonce[:], ct, inst.aad)
	if err != nil {
		return eh.Errorf("adhocdata: authenticate chunk: %w", err)
	}
	if !final && len(plain) != inst.chunkSize {
		return eh.Errorf("adhocdata: malformed non-final chunk: %d plaintext bytes, want %d", len(plain), inst.chunkSize)
	}
	inst.plain = plain
	inst.off = 0
	inst.done = final
	return
}

// atEOF reports whether the underlying stream has no more bytes, i.e.
// the chunk just read was the final one.
func (inst *Reader) atEOF() (eof bool, err error) {
	_, err = inst.br.Peek(1)
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, eh.Errorf("adhocdata: peek: %w", err)
	}
	return false, nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key.
func newGCM(key []byte) (aead cipher.AEAD, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, eh.Errorf("adhocdata: new cipher: %w", err)
	}
	aead, err = cipher.NewGCM(block)
	if err != nil {
		return nil, eh.Errorf("adhocdata: new gcm: %w", err)
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
			err = eh.Errorf("adhocdata: truncated stream: short header")
		} else {
			err = eh.Errorf("adhocdata: read header: %w", err)
		}
		return
	}
	if string(hdr[0:len(magic)]) != magic {
		err = eh.Errorf("adhocdata: bad magic")
		return
	}
	if hdr[len(magic)] != formatVersion {
		err = eh.Errorf("adhocdata: unsupported format version %d", hdr[len(magic)])
		return
	}
	cs := binary.LittleEndian.Uint32(hdr[len(magic)+1:])
	if cs == 0 || cs > maxChunkSize {
		err = eh.Errorf("adhocdata: chunk size %d out of range", cs)
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
