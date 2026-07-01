package internalized

import (
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

func TestBadgerIdInternalizer_RoundtripAndIdempotent(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	gen, err := genFac.Create(identifier.TagValue(3), 16)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	k := []byte("de305d54-75b4-431b-adb2-eb6b9e546013")
	id1, fresh1, err := gen.GetId(k)
	require.NoError(t, err)
	require.True(t, fresh1)
	require.True(t, id1.IsValid())
	require.EqualValues(t, 3, id1.GetTag().GetValue())

	id2, fresh2, err := gen.GetId(k)
	require.NoError(t, err)
	require.False(t, fresh2)
	require.Equal(t, id1, id2)
}

// TestBadgerIdInternalizer_TagIsolation is the regression test for the cross-tag
// key collision: the same natural key under two different tags in one store must
// map to two distinct ids, each carrying its own tag.
func TestBadgerIdInternalizer_TagIsolation(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	genA, err := genFac.Create(identifier.TagValue(1), 16)
	require.NoError(t, err)
	defer func() { _ = genA.Release() }()
	genB, err := genFac.Create(identifier.TagValue(2), 16)
	require.NoError(t, err)
	defer func() { _ = genB.Release() }()

	key := []byte("shared-key")
	idA, _, err := genA.GetId(key)
	require.NoError(t, err)
	idB, _, err := genB.GetId(key)
	require.NoError(t, err)

	require.NotEqual(t, idA, idB)
	require.EqualValues(t, 1, idA.GetTag().GetValue())
	require.EqualValues(t, 2, idB.GetTag().GetValue())

	// Each tag resolves its own key stably.
	idA2, freshA2, err := genA.GetId(key)
	require.NoError(t, err)
	require.False(t, freshA2)
	require.Equal(t, idA, idA2)
}

func TestBadgerIdInternalizer_RejectsEmptyKey(t *testing.T) {
	genFac, err := NewBadgerIdInternalizedGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()
	gen, err := genFac.Create(identifier.TagValue(1), 16)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	_, _, err = gen.GetId(nil)
	require.Error(t, err)
	_, _, err = gen.GetId([]byte{})
	require.Error(t, err)
}

func TestBadgerIdInternalizer_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	genFac, err := NewBadgerIdInternalizedGenerator(dir)
	require.NoError(t, err)
	gen, err := genFac.Create(identifier.TagValue(4), 16)
	require.NoError(t, err)
	key := []byte("persisted")
	id1, _, err := gen.GetId(key)
	require.NoError(t, err)
	require.NoError(t, gen.Release())
	require.NoError(t, genFac.Close())

	// Reopen the same store; the mapping must survive.
	genFac2, err := NewBadgerIdInternalizedGenerator(dir)
	require.NoError(t, err)
	defer func() { _ = genFac2.Close() }()
	gen2, err := genFac2.Create(identifier.TagValue(4), 16)
	require.NoError(t, err)
	defer func() { _ = gen2.Release() }()
	id2, fresh2, err := gen2.GetId(key)
	require.NoError(t, err)
	require.False(t, fresh2, "mapping should have persisted across reopen")
	require.Equal(t, id1, id2)
}
