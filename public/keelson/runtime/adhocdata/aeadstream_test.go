package adhocdata

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
// stream bytes.
func encryptChunk(t *testing.T, key []byte, chunkSize int, plaintext []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := newWriterChunk(&buf, key, chunkSize)
	if err != nil {
		t.Fatalf("newWriterChunk: %v", err)
	}
	if _, err = w.Write(plaintext); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err = w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

// decrypt reads a full stream back.
func decrypt(key, data []byte) ([]byte, error) {
	r, err := NewReader(bytes.NewReader(data), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestRoundtrip(t *testing.T) {
	key := testKey(1)
	// Small chunk size exercises the many-chunk paths cheaply; sizes
	// straddle every boundary class.
	const cs = 16
	for _, n := range []int{0, 1, cs - 1, cs, cs + 1, 2 * cs, 3*cs + 7, 100 * cs} {
		pt := pattern(n)
		stream := encryptChunk(t, key, cs, pt)
		got, err := decrypt(key, stream)
		if err != nil {
			t.Fatalf("n=%d: decrypt: %v", n, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("n=%d: roundtrip mismatch (got %d bytes)", n, len(got))
		}
	}
}

func TestRoundtripDefaultChunkSize(t *testing.T) {
	key := testKey(2)
	for _, n := range []int{0, 1, ChunkSize, ChunkSize + 1, 5 << 20} { // incl. ~5 MiB
		pt := pattern(n)
		var buf bytes.Buffer
		w, err := NewWriter(&buf, key)
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		// Drive several Write calls to prove chunking is independent of
		// call boundaries.
		for off := 0; off < len(pt); off += 4093 {
			end := min(off+4093, len(pt))
			if _, err = w.Write(pt[off:end]); err != nil {
				t.Fatalf("n=%d: write: %v", n, err)
			}
		}
		if err = w.Close(); err != nil {
			t.Fatalf("n=%d: close: %v", n, err)
		}
		got, err := decrypt(key, buf.Bytes())
		if err != nil {
			t.Fatalf("n=%d: decrypt: %v", n, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("n=%d: roundtrip mismatch", n)
		}
	}
}

func TestWrongKey(t *testing.T) {
	stream := encryptChunk(t, testKey(1), 16, pattern(40))
	if _, err := decrypt(testKey(9), stream); err == nil {
		t.Fatal("decrypt with wrong key must fail")
	}
}

// TestTruncationProperty asserts the core anti-truncation guarantee: no
// proper prefix of a valid stream ever decrypts. This subsumes the
// mid-header, mid-chunk, and missing-final boundary classes.
func TestTruncationProperty(t *testing.T) {
	key := testKey(3)
	const cs = 16
	stream := encryptChunk(t, key, cs, pattern(3*cs+5)) // several chunks
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
	stream := encryptChunk(t, key, cs, pattern(3*cs+5))
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
// chunk is detected (the final chunk then reads as non-final and fails
// authentication).
func TestExtraTrailingBytes(t *testing.T) {
	key := testKey(5)
	const cs = 16
	stream := encryptChunk(t, key, cs, pattern(cs+3))
	corrupt := append(append([]byte{}, stream...), 0x00, 0x00, 0x00, 0x01, 0xff)
	if _, err := decrypt(key, corrupt); err == nil {
		t.Fatal("trailing garbage after final chunk must be detected")
	}
}

func TestDeterminism(t *testing.T) {
	key := testKey(6)
	pt := pattern(3*ChunkSize + 123)
	a := encryptChunk(t, key, ChunkSize, pt)
	b := encryptChunk(t, key, ChunkSize, pt)
	if !bytes.Equal(a, b) {
		t.Fatal("same key + plaintext must produce identical ciphertext")
	}
}

// TestMissingCloseIsInvalid asserts that a stream whose writer was never
// closed lacks a final chunk and does not decrypt.
func TestMissingCloseIsInvalid(t *testing.T) {
	key := testKey(7)
	const cs = 16
	var buf bytes.Buffer
	w, err := newWriterChunk(&buf, key, cs)
	if err != nil {
		t.Fatalf("newWriterChunk: %v", err)
	}
	if _, err = w.Write(pattern(2 * cs)); err != nil { // forces a non-final chunk out
		t.Fatalf("write: %v", err)
	}
	// No Close.
	if _, err := decrypt(key, buf.Bytes()); err == nil {
		t.Fatal("stream without Close (no final chunk) must not decrypt")
	}
}

func TestKeySizeValidation(t *testing.T) {
	if _, err := NewWriter(io.Discard, make([]byte, 16)); err == nil {
		t.Fatal("NewWriter must reject a non-32-byte key")
	}
	if _, err := NewReader(bytes.NewReader(nil), make([]byte, 31)); err == nil {
		t.Fatal("NewReader must reject a non-32-byte key")
	}
}

func TestWriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testKey(8))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
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
