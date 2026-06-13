package persist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryBackend_GetSetDelete_RoundTrip(t *testing.T) {
	b := NewMemoryBackend()

	value, found, err := b.Get("play", "tabs")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, value)

	err = b.Set("play", "tabs", []byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 1, b.Len())

	got, found, err := b.Get("play", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("hello"), got)

	err = b.Delete("play", "tabs")
	require.NoError(t, err)
	assert.Equal(t, 0, b.Len())

	_, found, err = b.Get("play", "tabs")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestMemoryBackend_DefensiveCopyOnGet(t *testing.T) {
	b := NewMemoryBackend()
	stored := []byte("hello")
	err := b.Set("play", "tabs", stored)
	require.NoError(t, err)

	got, _, _ := b.Get("play", "tabs")
	got[0] = 'X'
	// Re-fetch: should still be "hello", not "Xello".
	again, _, _ := b.Get("play", "tabs")
	assert.Equal(t, "hello", string(again))
}

func TestMemoryBackend_DefensiveCopyOnSet(t *testing.T) {
	b := NewMemoryBackend()
	source := []byte("hello")
	err := b.Set("play", "tabs", source)
	require.NoError(t, err)
	source[0] = 'X'
	got, _, _ := b.Get("play", "tabs")
	assert.Equal(t, "hello", string(got))
}

func TestMemoryBackend_AliasSeparation(t *testing.T) {
	b := NewMemoryBackend()
	require.NoError(t, b.Set("play", "tabs", []byte("p")))
	require.NoError(t, b.Set("imztop", "tabs", []byte("i")))

	got, _, _ := b.Get("play", "tabs")
	assert.Equal(t, "p", string(got))
	got, _, _ = b.Get("imztop", "tabs")
	assert.Equal(t, "i", string(got))
	assert.Equal(t, 2, b.Len())
}
