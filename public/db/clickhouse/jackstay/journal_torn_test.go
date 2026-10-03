package jackstay

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appendRaw(t *testing.T, path string, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(s)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// A line a crash cut short is cut off when the journal is opened for
// writing, so the next entry starts a line of its own and the journal reads
// back whole.
func TestJournal_TornLineThenAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.journal")
	now := time.Unix(1, 0)
	j, err := OpenJournal(path, "r")
	require.NoError(t, err)
	require.NoError(t, j.RecordChunk("a.t", "0", SyncModeFull, leafDigest{n: 1, kd: 1, rd: 1}, now))
	require.NoError(t, j.Close())
	appendRaw(t, path, `{"run":"r","table":"a.t","ev`)

	j, err = OpenJournal(path, "r")
	require.NoError(t, err)
	require.NoError(t, j.RecordChunk("a.t", "1", SyncModeFull, leafDigest{n: 2, kd: 2, rd: 2}, now))
	require.NoError(t, j.Close())

	j, err = ReadJournal(path, "r")
	require.NoError(t, err)
	chunks, rows := j.DoneChunks("a.t")
	assert.Equal(t, 2, chunks)
	assert.Equal(t, uint64(3), rows)
}

// A last line that decodes but lost its newline is kept and terminated.
func TestJournal_UnterminatedLineThenAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.journal")
	require.NoError(t, os.WriteFile(path, []byte(`{"run":"r","table":"a.t","event":"attempt","chunk":"0","at":"1970-01-01T00:00:01Z"}`), 0o644))
	j, err := OpenJournal(path, "r")
	require.NoError(t, err)
	assert.True(t, j.Attempted("a.t", "0"))
	require.NoError(t, j.RecordAttempt("a.t", "1", time.Unix(1, 0)))
	require.NoError(t, j.Close())

	j, err = ReadJournal(path, "r")
	require.NoError(t, err)
	assert.True(t, j.Attempted("a.t", "0"))
	assert.True(t, j.Attempted("a.t", "1"))
}

// Reading alone repairs nothing, and a bad line before the last is not a
// journal this code wrote.
func TestJournal_ReadOnlyAndCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.journal")
	torn := `{"run":"r","table":"a.t","event":"attempt","chunk":"0","at":"1970-01-01T00:00:01Z"}` + "\n" + `{"run":"r"`
	require.NoError(t, os.WriteFile(path, []byte(torn), 0o644))
	j, err := ReadJournal(path, "r")
	require.NoError(t, err)
	assert.True(t, j.Attempted("a.t", "0"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, torn, string(data))

	require.NoError(t, os.WriteFile(path, []byte("garbage\n"+`{"run":"r","table":"a.t","event":"attempt","chunk":"0","at":"1970-01-01T00:00:01Z"}`+"\n"), 0o644))
	_, err = ReadJournal(path, "r")
	assert.ErrorContains(t, err, "unable to decode journal line")
}
