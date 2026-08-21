package ladingview

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
