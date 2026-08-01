package adhocdata

import (
	"bytes"
	"io"
	"testing"
)

// seekFixture encrypts n pseudo-random plaintext bytes with a small
// chunk size (many chunks without large payloads) and returns the
// plaintext and ciphertext.
func seekFixture(t *testing.T, n, chunkSize int) (plain, ct []byte, key []byte) {
	t.Helper()
	key = bytes.Repeat([]byte{7}, KeySize)
	plain = make([]byte, n)
	for i := range plain {
		plain[i] = byte(i*131 + i>>8)
	}
	var buf bytes.Buffer
	w, err := newWriterChunk(&buf, key, chunkSize)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if _, err = w.Write(plain); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err = w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ct = buf.Bytes()
	return
}

func newSeekable(t *testing.T, ct, key []byte) *SeekableReader {
	t.Helper()
	sr, err := NewSeekableReader(bytes.NewReader(ct), int64(len(ct)), key)
	if err != nil {
		t.Fatalf("seekable reader: %v", err)
	}
	return sr
}

// TestSeekableReaderRoundTrip pins full reads and offset reads against
// the reference plaintext across the chunk-boundary size cases: empty,
// sub-chunk, exact multiples (the final chunk may legally be full-size),
// off-by-one around boundaries.
func TestSeekableReaderRoundTrip(t *testing.T) {
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

		// Offset reads, including chunk-straddling ones.
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
		}
	}
}

func TestSeekableReaderSeekSemantics(t *testing.T) {
	const cs, n = 64, 3*64 + 7
	plain, ct, key := seekFixture(t, n, cs)
	sr := newSeekable(t, ct, key)

	// SeekEnd: read the tail.
	pos, err := sr.Seek(-7, io.SeekEnd)
	if err != nil || pos != int64(n-7) {
		t.Fatalf("seek end: pos=%d err=%v", pos, err)
	}
	got, err := io.ReadAll(sr)
	if err != nil || !bytes.Equal(got, plain[n-7:]) {
		t.Fatalf("tail read: %q err=%v", got, err)
	}

	// SeekCurrent composes.
	_, _ = sr.Seek(10, io.SeekStart)
	pos, err = sr.Seek(5, io.SeekCurrent)
	if err != nil || pos != 15 {
		t.Fatalf("seek current: pos=%d err=%v", pos, err)
	}

	// Beyond the end is legal; reads return EOF.
	if _, err = sr.Seek(int64(n)+100, io.SeekStart); err != nil {
		t.Fatalf("seek beyond end: %v", err)
	}
	if _, err = sr.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("read beyond end: %v, want EOF", err)
	}

	// Negative is an error and does not move the position.
	if _, err = sr.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek accepted")
	}
}

// TestSeekableReaderAgreesWithStreamReader cross-checks the two readers
// over the same ciphertext, so the seekable geometry cannot drift from
// the streaming truth.
func TestSeekableReaderAgreesWithStreamReader(t *testing.T) {
	_, ct, key := seekFixture(t, 5*64+13, 64)
	stream, err := NewReader(bytes.NewReader(ct), key)
	if err != nil {
		t.Fatalf("stream reader: %v", err)
	}
	want, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("stream read: %v", err)
	}
	got, err := io.ReadAll(newSeekable(t, ct, key))
	if err != nil {
		t.Fatalf("seekable read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("seekable and streaming reads disagree")
	}
}

func TestSeekableReaderRejectsTampering(t *testing.T) {
	const cs = 64
	_, ct, key := seekFixture(t, 4*cs+9, cs)

	// Flip one ciphertext byte inside chunk 2's payload.
	tampered := append([]byte(nil), ct...)
	chunk2 := headerSize + 2*(lenPrefixSize+cs+tagSize) + lenPrefixSize + 3
	tampered[chunk2] ^= 0x40
	sr := newSeekable(t, tampered, key)

	// Chunks before the tamper still read.
	buf := make([]byte, cs)
	if _, err := io.ReadFull(sr, buf); err != nil {
		t.Fatalf("chunk 0: %v", err)
	}
	// Seeking into the tampered chunk fails authentication, and the
	// failure is sticky.
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

func TestSeekableReaderRejectsBadGeometry(t *testing.T) {
	_, ct, key := seekFixture(t, 200, 64)

	// Truncation is rejected no later than the first read that touches
	// it: a cut that still fits the chunk arithmetic re-shapes the final
	// chunk, whose stored length prefix then contradicts the geometry.
	for _, cut := range []int{1, 5, tagSize + lenPrefixSize, 100} {
		short := ct[:len(ct)-cut]
		sr, err := NewSeekableReader(bytes.NewReader(short), int64(len(short)), key)
		if err != nil {
			continue // rejected at construction — fine
		}
		if _, err = io.ReadAll(sr); err == nil {
			t.Fatalf("cut=%d: truncated ciphertext read to completion", cut)
		}
	}
	// Header-only is missing its final chunk.
	if _, err := NewSeekableReader(bytes.NewReader(ct[:headerSize]), int64(headerSize), key); err == nil {
		t.Fatal("header-only ciphertext accepted")
	}
}
