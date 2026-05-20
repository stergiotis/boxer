//go:build llm_generated_opus47

package diskbacked

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTempPogreb(t *testing.T, softCap int) *PogrebStash[string, int] {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pogreb")
	s, err := NewPogrebStash[string, int](dir, softCap, true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPogrebStash_BasicRoundTrip(t *testing.T) {
	s := openTempPogreb(t, 0)

	assert.False(t, s.Add("a", 1))
	assert.False(t, s.Add("b", 2))
	assert.Equal(t, 2, s.Len())
	assert.Equal(t, 0, s.Cap(), "softCap=0 → unbounded")

	v, has := s.GetAndRemove("a")
	assert.True(t, has)
	assert.Equal(t, 1, v)
	assert.Equal(t, 1, s.Len(), "Len decremented on successful GetAndRemove")

	// Second GetAndRemove for the same key is a miss.
	_, has = s.GetAndRemove("a")
	assert.False(t, has)
	assert.Equal(t, 1, s.Len(), "miss does not decrement")
}

func TestPogrebStash_SoftCapEviction(t *testing.T) {
	s := openTempPogreb(t, 2)

	assert.False(t, s.Add("a", 1))
	assert.False(t, s.Add("b", 2))
	assert.Equal(t, 2, s.Len())

	// At cap. Next Add must evict one and report it.
	assert.True(t, s.Add("c", 3), "Add at softCap must evict")
	assert.Equal(t, 2, s.Len(), "Len stays at softCap after eviction")

	// The new key "c" is reachable; the surviving original is one of {a, b}.
	v, has := s.GetAndRemove("c")
	assert.True(t, has)
	assert.Equal(t, 3, v)
}

func TestPogrebStash_Delete(t *testing.T) {
	s := openTempPogreb(t, 0)
	s.Add("a", 1)
	s.Add("b", 2)
	assert.Equal(t, 2, s.Len())

	s.Delete("a")
	assert.Equal(t, 1, s.Len())

	// Deleting a missing key is a no-op for Len (pogreb's Delete is idempotent).
	s.Delete("ghost")
	assert.Equal(t, 1, s.Len())

	// Surviving key still readable.
	v, has := s.GetAndRemove("b")
	assert.True(t, has)
	assert.Equal(t, 2, v)
}

func TestPogrebStash_ReopenPreservesEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pogreb")

	s1, err := NewPogrebStash[string, int](dir, 0, true)
	require.NoError(t, err)
	s1.Add("a", 1)
	s1.Add("b", 2)
	s1.Add("c", 3)
	require.NoError(t, s1.Close())

	// Reopen without cleanStart — pogreb's Count() is live, so Len()
	// reflects on-disk entries immediately.
	s2, err := NewPogrebStash[string, int](dir, 0, false)
	require.NoError(t, err)
	defer s2.Close()
	assert.Equal(t, 3, s2.Len(), "reopened stash must reflect on-disk entries")

	v, has := s2.GetAndRemove("b")
	assert.True(t, has)
	assert.Equal(t, 2, v)
	assert.Equal(t, 2, s2.Len())
}
