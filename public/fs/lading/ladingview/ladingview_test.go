package ladingview

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/fsmatch"
)

func TestReadHeadWholeAndTruncated(t *testing.T) {
	fsys := fstest.MapFS{
		"small.txt": {Data: []byte("hello")},
		"big.bin":   {Data: make([]byte, 10_000)},
		"d/x":       {Data: []byte("x")},
	}
	h, err := ReadHead(fsys, "small.txt", 1024, 16)
	require.NoError(t, err)
	assert.Equal(t, int64(5), h.Size)
	assert.Equal(t, "hello", string(h.Data))
	assert.False(t, h.Truncated)

	h, err = ReadHead(fsys, "big.bin", 1024, 16)
	require.NoError(t, err)
	assert.Equal(t, int64(10_000), h.Size)
	assert.Len(t, h.Data, 16)
	assert.True(t, h.Truncated)

	h, err = ReadHead(fsys, "d", 1024, 16)
	require.NoError(t, err)
	assert.True(t, h.IsDir)
	assert.Nil(t, h.Data)

	_, err = ReadHead(fsys, "nope", 1024, 16)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestLockedIsAnFS(t *testing.T) {
	var g Guard
	inner := fstest.MapFS{"a/b.txt": {Data: []byte("ab")}, "a/c.txt": {Data: []byte("c")}}
	l := NewLocked(&g, inner)
	require.NoError(t, fstest.TestFS(l, "a/b.txt", "a/c.txt"))
	es, err := l.ReadDir("a")
	require.NoError(t, err)
	assert.Len(t, es, 2)
	data, err := l.ReadFile("a/b.txt")
	require.NoError(t, err)
	assert.Equal(t, "ab", string(data))
	// The guard is free after every call.
	assert.True(t, g.mu.TryLock())
	g.mu.Unlock()
}

// pushdownFS is an fs that answers the fsmatch seam itself, so the test can
// see the call arrive, and under the guard.
type pushdownFS struct {
	fstest.MapFS
	g          *Guard
	calls      int
	underGuard bool
}

func (p *pushdownFS) MatchPaths(dir, pattern string, hidden bool, limit int) ([]fsmatch.Match, bool, error) {
	p.calls++
	p.underGuard = !p.g.mu.TryLock()
	info, _ := fs.Stat(p.MapFS, "a/b.txt")
	return []fsmatch.Match{{Path: "a/b.txt", Info: info}}, false, nil
}

func TestLockedForwardsMatchPaths(t *testing.T) {
	var g Guard
	plain := NewLocked(&g, fstest.MapFS{"a/b.txt": {Data: []byte("ab")}})
	_, _, err := plain.MatchPaths(".", "b", true, 0)
	assert.ErrorIs(t, err, errors.ErrUnsupported, "a file system without the seam says so rather than answering nothing")

	inner := &pushdownFS{MapFS: fstest.MapFS{"a/b.txt": {Data: []byte("ab")}}, g: &g}
	l := NewLocked(&g, inner)
	got, more, err := l.MatchPaths(".", "b", true, 0)
	require.NoError(t, err)
	assert.False(t, more)
	assert.Equal(t, 1, inner.calls)
	assert.True(t, inner.underGuard, "the call arrived under the guard")
	require.Len(t, got, 1)
	assert.Equal(t, "a/b.txt", got[0].Path)
	assert.True(t, g.mu.TryLock(), "and the guard is free afterwards")
	g.mu.Unlock()
}
