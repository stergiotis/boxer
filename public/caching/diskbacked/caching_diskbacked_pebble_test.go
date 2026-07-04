package diskbacked

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTempPebble(t *testing.T, softCap int) *PebbleStash[string, int] {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pebble")
	s, err := NewPebbleStash[string, int](dir, softCap, true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPebbleStash_BasicRoundTrip(t *testing.T) {
	s := openTempPebble(t, 0)

	assert.False(t, s.Add("a", 1, false))
	assert.False(t, s.Add("b", 2, false))
	assert.Equal(t, 2, s.Len())
	assert.Equal(t, 0, s.Cap(), "softCap=0 → unbounded")

	v, _, has := s.GetAndRemove("a")
	assert.True(t, has)
	assert.Equal(t, 1, v)
	assert.Equal(t, 1, s.Len(), "Len decremented on successful GetAndRemove")

	// Second GetAndRemove for the same key is a miss.
	_, _, has = s.GetAndRemove("a")
	assert.False(t, has)
	assert.Equal(t, 1, s.Len(), "miss does not decrement")
}

func TestPebbleStash_SoftCapEviction(t *testing.T) {
	s := openTempPebble(t, 2)

	assert.False(t, s.Add("a", 1, false))
	assert.False(t, s.Add("b", 2, false))
	assert.Equal(t, 2, s.Len())

	// At cap. Next Add must evict one and report it.
	assert.True(t, s.Add("c", 3, false), "Add at softCap must evict")
	assert.Equal(t, 2, s.Len(), "Len stays at softCap after eviction")

	// Update of an existing key never evicts and never grows count.
	assert.False(t, s.Add("c", 33, false), "update of present key does not evict")
	assert.Equal(t, 2, s.Len())
	v, _, has := s.GetAndRemove("c")
	assert.True(t, has)
	assert.Equal(t, 33, v, "update overwrote the value")
}

func TestPebbleStash_Delete(t *testing.T) {
	s := openTempPebble(t, 0)
	s.Add("a", 1, false)
	s.Add("b", 2, false)
	assert.Equal(t, 2, s.Len())

	s.Delete("a")
	assert.Equal(t, 1, s.Len())

	// Deleting a missing key is a no-op for Len.
	s.Delete("ghost")
	assert.Equal(t, 1, s.Len())
}

func TestPebbleStash_ReopenCountsExisting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pebble")

	s1, err := NewPebbleStash[string, int](dir, 0, true)
	require.NoError(t, err)
	s1.Add("a", 1, false)
	s1.Add("b", 2, false)
	s1.Add("c", 3, false)
	require.NoError(t, s1.Close())

	// Reopen without cleanStart — must scan and seed count to 3.
	s2, err := NewPebbleStash[string, int](dir, 0, false)
	require.NoError(t, err)
	defer s2.Close()
	assert.Equal(t, 3, s2.Len(), "reopened stash must reflect on-disk entries")
}
