package sealed

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testingNow is a monotonic clock in nanoseconds for the linearity test.
func testingNow() int64 { return time.Now().UnixNano() }

func sealBytes(t *testing.T, dir string, pt []byte) (f *File) {
	t.Helper()
	f, err := CreateIn(dir)
	require.NoError(t, err)
	w, err := f.Writer()
	require.NoError(t, err)
	_, err = w.Write(pt)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return f
}

// TestFileHasNoName is the ADR-0240 §SD1 property: the base directory
// holds no entry while the file is live, and nothing appears after a
// simulated crash (the descriptor dropped without any cleanup).
func TestFileHasNoName(t *testing.T) {
	dir := t.TempDir()
	f := sealBytes(t, dir, pattern(3*ChunkSize+11))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "an unnamed file leaves no directory entry")

	r, err := f.Open()
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, pattern(3*ChunkSize+11), got)
	require.NoError(t, r.Close())

	// "Crash": drop the file without Close/Retire. The kernel owns the
	// reclamation; what we can assert is that there is still nothing to
	// find.
	f = nil //nolint:ineffassign // the point is the absence of a name
	entries, err = os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestFileCiphertextIsOpaque(t *testing.T) {
	pt := bytes.Repeat([]byte("RIFF-WAVE-recognisable-plaintext-"), 100)
	f := sealBytes(t, t.TempDir(), pt)
	ct := make([]byte, f.Size())
	_, err := f.ciphertextAt(ct, 0)
	require.NoError(t, err)
	assert.Equal(t, "BXAD", string(ct[:4]))
	assert.False(t, bytes.Contains(ct, []byte("RIFF")), "the sealed bytes carry no plaintext")
	assert.Equal(t, int64(len(pt)), f.PlaintextSize())
}

func TestFileLifecycle(t *testing.T) {
	f, err := CreateIn(t.TempDir())
	require.NoError(t, err)
	_, err = f.Open()
	require.Error(t, err, "not readable before the writer closes")
	w, err := f.Writer()
	require.NoError(t, err)
	_, err = f.Writer()
	require.Error(t, err, "written exactly once")
	_, err = w.Write(pattern(10))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r1, err := f.Open()
	require.NoError(t, err)
	r2, err := f.Open()
	require.NoError(t, err)
	assert.Equal(t, 2, f.Readers())

	// Retire with readers open: waits for them.
	f.Retire(time.Minute)
	assert.False(t, f.Closed())
	require.NoError(t, r1.Close())
	assert.False(t, f.Closed())
	require.NoError(t, r2.Close())
	assert.True(t, f.Closed(), "the last reader's close retires the file")
	_, err = f.Open()
	require.Error(t, err, "no opens after close")
	f.Retire(time.Minute) // idempotent
	require.NoError(t, f.Close())
}

func TestFileRetireCeiling(t *testing.T) {
	// Several chunks, so that a read after the close has to touch the
	// descriptor rather than the reader's cached final chunk.
	f := sealBytes(t, t.TempDir(), pattern(3*ChunkSize+5))
	r, err := f.Open()
	require.NoError(t, err)
	f.Retire(20 * time.Millisecond)
	require.Eventually(t, f.Closed, time.Second, time.Millisecond, "a reader that never leaves cannot pin the file past the ceiling")
	_, err = r.ReadAt(make([]byte, 1), 0)
	require.Error(t, err, "the stranded reader fails rather than reading a closed descriptor")
	require.NoError(t, r.Close())
	assert.Equal(t, 0, f.Readers())
}

func TestFileRetireIdle(t *testing.T) {
	f := sealBytes(t, t.TempDir(), pattern(10))
	f.Retire(time.Hour)
	assert.True(t, f.Closed(), "no readers: retired at once")
}

func TestFileConcurrentReaders(t *testing.T) {
	pt := pattern(5*ChunkSize + 17)
	f := sealBytes(t, t.TempDir(), pt)
	r, err := f.Open()
	require.NoError(t, err)
	done := make(chan error, 8)
	for g := range 8 {
		go func() {
			buf := make([]byte, 1000)
			for i := 0; i < 50; i++ {
				off := int64((g*7919 + i*104729) % (len(pt) - len(buf)))
				if _, err := r.ReadAt(buf, off); err != nil {
					done <- err
					return
				}
				if !bytes.Equal(buf, pt[off:off+int64(len(buf))]) {
					done <- errors.New("mismatch")
					return
				}
			}
			done <- nil
		}()
	}
	for range 8 {
		require.NoError(t, <-done)
	}
	require.NoError(t, r.Close())
}

func TestCreateInUnsupportedFilesystem(t *testing.T) {
	// /proc cannot allocate inodes; O_TMPFILE on it is refused.
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Skip("no procfs")
	}
	_, err := CreateIn("/proc/self")
	require.Error(t, err)
}
