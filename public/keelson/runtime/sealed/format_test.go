package sealed

import (
	"bytes"
	"io"
	"testing"
)

// testKey is a fixed 32-byte key; tests want reproducibility, not
// secrecy.
func testKey(seed byte) []byte {
	k := make([]byte, KeySize)
	for i := range k {
		k[i] = seed + byte(i)
	}
	return k
}

// pattern returns n deterministic bytes.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + 11)
	}
	return b
}

// encryptChunk seals plaintext with the given chunk size and returns the
// stream bytes, driving Write in pieces of writeStep bytes (0 = one call)
// to prove chunking is independent of call boundaries.
func encryptChunk(t *testing.T, key []byte, chunkSize int, plaintext []byte, writeStep int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := newWriter(&buf, key, chunkSize)
	if err != nil {
		t.Fatalf("newWriter: %v", err)
	}
	if writeStep <= 0 {
		writeStep = len(plaintext) + 1
	}
	for off := 0; off < len(plaintext); off += writeStep {
		end := min(off+writeStep, len(plaintext))
		if _, err = w.Write(plaintext[off:end]); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

// decrypt reads a full stream back through the seekable reader — the
// format's one reader.
func decrypt(key, data []byte) ([]byte, error) {
	r, err := newReader(bytes.NewReader(data), int64(len(data)), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestRoundtrip(t *testing.T) {
	key := testKey(1)
	const cs = 16
	for _, n := range []int{0, 1, cs - 1, cs, cs + 1, 2 * cs, 3*cs + 7, 100 * cs} {
		for _, step := range []int{0, 1, cs - 1, cs, cs + 1, 5*cs + 3} {
			pt := pattern(n)
			stream := encryptChunk(t, key, cs, pt, step)
			got, err := decrypt(key, stream)
			if err != nil {
				t.Fatalf("n=%d step=%d: decrypt: %v", n, step, err)
			}
			if !bytes.Equal(got, pt) {
				t.Fatalf("n=%d step=%d: roundtrip mismatch (got %d bytes)", n, step, len(got))
			}
		}
	}
}

func TestRoundtripDefaultChunkSize(t *testing.T) {
	key := testKey(2)
	for _, n := range []int{0, 1, ChunkSize, ChunkSize + 1, 5 << 20} { // incl. ~5 MiB
		pt := pattern(n)
		stream := encryptChunk(t, key, ChunkSize, pt, 4093)
		got, err := decrypt(key, stream)
		if err != nil {
			t.Fatalf("n=%d: decrypt: %v", n, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("n=%d: roundtrip mismatch", n)
		}
	}
}

// TestWriterIsLinear pins the ADR-0240 §SD1 writer shape: one large Write
// costs one pass, so a many-chunk write must not be dramatically slower
// per byte than a chunk-sized one. Measured as a ratio, so machine speed
// does not matter; the quadratic predecessor was off by three orders of
// magnitude at this size.
func TestWriterIsLinear(t *testing.T) {
	key := testKey(3)
	const cs = 4096
	small := pattern(cs)
	large := pattern(256 * cs)
	perByte := func(pt []byte) float64 {
		var buf bytes.Buffer
		w, err := newWriter(&buf, key, cs)
		if err != nil {
			t.Fatal(err)
		}
		start := testingNow()
		for range 8 {
			if _, err = w.Write(pt); err != nil {
				t.Fatal(err)
			}
		}
		return float64(testingNow()-start) / float64(8*len(pt))
	}
	s, l := perByte(small), perByte(large)
	if l > 8*s+1 { // +1 ns absorbs timer granularity on the tiny case
		t.Fatalf("large write costs %.2f ns/byte, small %.2f ns/byte: not linear", l, s)
	}
}

func TestWrongKey(t *testing.T) {
	stream := encryptChunk(t, testKey(1), 16, pattern(40), 0)
	if _, err := decrypt(testKey(9), stream); err == nil {
		t.Fatal("decrypt with wrong key must fail")
	}
}

// TestTruncationProperty asserts the core anti-truncation guarantee: no
// proper prefix of a valid stream ever decrypts.
func TestTruncationProperty(t *testing.T) {
	key := testKey(3)
	const cs = 16
	stream := encryptChunk(t, key, cs, pattern(3*cs+5), 0)
	if _, err := decrypt(key, stream); err != nil {
		t.Fatalf("full stream must decrypt: %v", err)
	}
	for l := range len(stream) {
		if _, err := decrypt(key, stream[:l]); err == nil {
			t.Fatalf("truncated prefix of length %d must not decrypt", l)
		}
	}
}

// TestBitFlip asserts that flipping any single bit anywhere in the
// stream — header, length prefix, ciphertext, or tag — is detected.
func TestBitFlip(t *testing.T) {
	key := testKey(4)
	const cs = 16
	stream := encryptChunk(t, key, cs, pattern(3*cs+5), 0)
	for i := range len(stream) {
		corrupt := make([]byte, len(stream))
		copy(corrupt, stream)
		corrupt[i] ^= 0x01
		if _, err := decrypt(key, corrupt); err == nil {
			t.Fatalf("flipped bit at byte %d must be detected", i)
		}
	}
}

// TestExtraTrailingBytes asserts that appending bytes past the final
// chunk is detected: the geometry no longer fits, or the final chunk
// reads as non-final and fails authentication.
func TestExtraTrailingBytes(t *testing.T) {
	key := testKey(5)
	const cs = 16
	stream := encryptChunk(t, key, cs, pattern(cs+3), 0)
	for _, extra := range [][]byte{{0x00}, {0x00, 0x00, 0x00, 0x01, 0xff}, pattern(lenPrefixSize + cs + tagSize)} {
		corrupt := append(append([]byte{}, stream...), extra...)
		if _, err := decrypt(key, corrupt); err == nil {
			t.Fatalf("%d trailing bytes after the final chunk must be detected", len(extra))
		}
	}
}

func TestDeterminism(t *testing.T) {
	key := testKey(6)
	pt := pattern(3*ChunkSize + 123)
	a := encryptChunk(t, key, ChunkSize, pt, 0)
	b := encryptChunk(t, key, ChunkSize, pt, 777)
	if !bytes.Equal(a, b) {
		t.Fatal("same key + plaintext must produce identical ciphertext regardless of write boundaries")
	}
}

// TestMissingCloseIsInvalid asserts that a stream whose writer was never
// closed lacks a final chunk and does not decrypt.
func TestMissingCloseIsInvalid(t *testing.T) {
	key := testKey(7)
	const cs = 16
	var buf bytes.Buffer
	w, err := newWriter(&buf, key, cs)
	if err != nil {
		t.Fatalf("newWriter: %v", err)
	}
	if _, err = w.Write(pattern(2*cs + 1)); err != nil { // forces non-final chunks out
		t.Fatalf("write: %v", err)
	}
	if _, err := decrypt(key, buf.Bytes()); err == nil {
		t.Fatal("stream without Close (no final chunk) must not decrypt")
	}
}

func TestKeySizeValidation(t *testing.T) {
	if _, err := newWriter(io.Discard, make([]byte, 16), ChunkSize); err == nil {
		t.Fatal("newWriter must reject a non-32-byte key")
	}
	if _, err := newReader(bytes.NewReader(nil), 0, make([]byte, 31)); err == nil {
		t.Fatal("newReader must reject a non-32-byte key")
	}
}

func TestWriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := newWriter(&buf, testKey(8), ChunkSize)
	if err != nil {
		t.Fatalf("newWriter: %v", err)
	}
	if err = w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err = w.Write([]byte("x")); err == nil {
		t.Fatal("write after close must fail")
	}
	if err = w.Close(); err != nil { // idempotent
		t.Fatalf("second close must be nil, got %v", err)
	}
}

// seekFixture seals n pseudo-random plaintext bytes with a small chunk
// size and returns the plaintext, ciphertext and key.
func seekFixture(t *testing.T, n, chunkSize int) (plain, ct []byte, key []byte) {
	t.Helper()
	key = bytes.Repeat([]byte{7}, KeySize)
	plain = make([]byte, n)
	for i := range plain {
		plain[i] = byte(i*131 + i>>8)
	}
	ct = encryptChunk(t, key, chunkSize, plain, 0)
	return
}

func newSeekable(t *testing.T, ct, key []byte) *Reader {
	t.Helper()
	sr, err := newReader(bytes.NewReader(ct), int64(len(ct)), key)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	return sr
}

// TestReaderRoundTrip pins full reads, offset reads and ReadAt against
// the reference plaintext across the chunk-boundary size cases.
func TestReaderRoundTrip(t *testing.T) {
	const cs = 64
	for _, n := range []int{0, 1, cs - 1, cs, cs + 1, 2 * cs, 3*cs + 7} {
		plain, ct, key := seekFixture(t, n, cs)
		sr := newSeekable(t, ct, key)
		if sr.PlaintextSize() != int64(n) {
			t.Fatalf("n=%d: plaintext size = %d", n, sr.PlaintextSize())
		}
		got, err := io.ReadAll(sr)
		if err != nil {
			t.Fatalf("n=%d: read all: %v", n, err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("n=%d: full read mismatch", n)
		}
		for _, off := range []int{0, 1, cs - 1, cs, cs + 1, n - 1, n} {
			if off < 0 || off > n {
				continue
			}
			if _, err = sr.Seek(int64(off), io.SeekStart); err != nil {
				t.Fatalf("n=%d off=%d: seek: %v", n, off, err)
			}
			got, err = io.ReadAll(sr)
			if err != nil {
				t.Fatalf("n=%d off=%d: read: %v", n, off, err)
			}
			if !bytes.Equal(got, plain[off:]) {
				t.Fatalf("n=%d off=%d: suffix mismatch", n, off)
			}
			// ReadAt spans chunks and reports EOF only on a short fill.
			buf := make([]byte, n-off)
			m, err := sr.ReadAt(buf, int64(off))
			if err != nil && !(err == io.EOF && off == n) {
				t.Fatalf("n=%d off=%d: readat: %v", n, off, err)
			}
			if !bytes.Equal(buf[:m], plain[off:]) {
				t.Fatalf("n=%d off=%d: readat mismatch", n, off)
			}
			if _, err = sr.ReadAt(make([]byte, 1), int64(n)); err != io.EOF {
				t.Fatalf("n=%d: readat past end: %v, want EOF", n, err)
			}
		}
	}
}

func TestReaderSeekSemantics(t *testing.T) {
	const cs, n = 64, 3*64 + 7
	plain, ct, key := seekFixture(t, n, cs)
	sr := newSeekable(t, ct, key)

	pos, err := sr.Seek(-7, io.SeekEnd)
	if err != nil || pos != int64(n-7) {
		t.Fatalf("seek end: pos=%d err=%v", pos, err)
	}
	got, err := io.ReadAll(sr)
	if err != nil || !bytes.Equal(got, plain[n-7:]) {
		t.Fatalf("tail read: %q err=%v", got, err)
	}
	_, _ = sr.Seek(10, io.SeekStart)
	pos, err = sr.Seek(5, io.SeekCurrent)
	if err != nil || pos != 15 {
		t.Fatalf("seek current: pos=%d err=%v", pos, err)
	}
	if _, err = sr.Seek(int64(n)+100, io.SeekStart); err != nil {
		t.Fatalf("seek beyond end: %v", err)
	}
	if _, err = sr.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("read beyond end: %v, want EOF", err)
	}
	if _, err = sr.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek accepted")
	}
	if err = sr.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = sr.Read(make([]byte, 1)); err == nil {
		t.Fatal("read after close accepted")
	}
}

func TestReaderRejectsTampering(t *testing.T) {
	const cs = 64
	_, ct, key := seekFixture(t, 4*cs+9, cs)
	tampered := append([]byte(nil), ct...)
	chunk2 := headerSize + 2*(lenPrefixSize+cs+tagSize) + lenPrefixSize + 3
	tampered[chunk2] ^= 0x40
	sr := newSeekable(t, tampered, key)

	buf := make([]byte, cs)
	if _, err := io.ReadFull(sr, buf); err != nil {
		t.Fatalf("chunk 0: %v", err)
	}
	if _, err := sr.Seek(int64(2*cs), io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := sr.Read(buf); err == nil {
		t.Fatal("tampered chunk read succeeded")
	}
	if _, err := sr.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek back: %v", err)
	}
	if _, err := sr.Read(buf); err == nil {
		t.Fatal("reader recovered after authentication failure")
	}
}

func TestReaderRejectsBadGeometry(t *testing.T) {
	_, ct, key := seekFixture(t, 200, 64)
	for _, cut := range []int{1, 5, tagSize + lenPrefixSize, 100} {
		short := ct[:len(ct)-cut]
		sr, err := newReader(bytes.NewReader(short), int64(len(short)), key)
		if err != nil {
			continue // rejected at construction — fine
		}
		if _, err = io.ReadAll(sr); err == nil {
			t.Fatalf("cut=%d: truncated ciphertext read to completion", cut)
		}
	}
	if _, err := newReader(bytes.NewReader(ct[:headerSize]), int64(headerSize), key); err == nil {
		t.Fatal("header-only ciphertext accepted")
	}
}
