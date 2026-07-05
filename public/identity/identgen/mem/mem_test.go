package mem

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

func TestNewIdInternalizer_RejectsInvalidTagValue(t *testing.T) {
	// Zero is the one invalid TagValue under fibonacci-coded tags (ADR-0106).
	_, err := NewIdInternalizer(identifier.TagValue(0), 0)
	require.Error(t, err)
}

// TestIdInternalizer_RejectsEmptyKey pins the contract shared with the Badger
// backend: a nil or zero-length natural key is rejected, not assigned an id.
func TestIdInternalizer_RejectsEmptyKey(t *testing.T) {
	s, err := NewIdInternalizer(identifier.TagValue(1), 0)
	require.NoError(t, err)

	_, _, err = s.GetId(nil)
	require.ErrorIs(t, err, identgen.ErrEmptyNaturalKey)
	_, _, err = s.GetId([]byte{})
	require.ErrorIs(t, err, identgen.ErrEmptyNaturalKey)
	_, _, err = s.GetUntaggedId(nil)
	require.ErrorIs(t, err, identgen.ErrEmptyNaturalKey)

	require.Equal(t, 0, s.Len())
}

func TestIdInternalizer_AssignsDenseMonotonicIds(t *testing.T) {
	s, err := NewIdInternalizer(identifier.TagValue(3), 4)
	require.NoError(t, err)
	for i := range 5 {
		id, fresh, err := s.GetId([]byte{byte('a' + i)})
		require.NoError(t, err)
		require.True(t, fresh)
		require.True(t, id.IsValid())
		_, untagged := id.Split()
		require.EqualValues(t, i+1, untagged) // body counter starts at 1; 0 stays reserved
		require.EqualValues(t, 3, id.GetTag().GetValue())
	}
	require.Equal(t, 5, s.Len())
}

func TestIdInternalizer_ReStampIsIdempotent(t *testing.T) {
	s, err := NewIdInternalizer(identifier.TagValue(1), 0)
	require.NoError(t, err)
	k := []byte("de305d54-75b4-431b-adb2-eb6b9e546013")
	id1, fresh1, err := s.GetId(k)
	require.NoError(t, err)
	require.True(t, fresh1)
	id2, fresh2, err := s.GetId(k)
	require.NoError(t, err)
	require.False(t, fresh2)
	require.Equal(t, id1, id2)
	require.Equal(t, 1, s.Len())
}

func TestIdInternalizer_ResolveRoundtrip(t *testing.T) {
	s, err := NewIdInternalizer(identifier.TagValue(5), 0)
	require.NoError(t, err)
	id, _, err := s.GetId([]byte("hello"))
	require.NoError(t, err)

	got, found := s.Resolve(id)
	require.True(t, found)
	require.Equal(t, "hello", got)

	_, untagged := id.Split()
	gotU, foundU := s.ResolveUntagged(untagged)
	require.True(t, foundU)
	require.Equal(t, "hello", gotU)

	// Unknown body id -> not found.
	_, found = s.ResolveUntagged(9999)
	require.False(t, found)

	// Same body value under a different tag must not resolve here.
	other := identifier.TagValue(6).GetTag().ComposeId(untagged)
	_, found = s.Resolve(other)
	require.False(t, found)
}

func TestIdInternalizer_AllYieldsAssignmentOrder(t *testing.T) {
	s, err := NewIdInternalizer(identifier.TagValue(2), 0)
	require.NoError(t, err)
	keys := []string{"one", "two", "three"}
	want := make([]identifier.TaggedId, 0, len(keys))
	for _, k := range keys {
		id, _, err := s.GetId([]byte(k))
		require.NoError(t, err)
		want = append(want, id)
	}
	i := 0
	for id, k := range s.All() {
		require.Equal(t, want[i], id)
		require.Equal(t, keys[i], k)
		i++
	}
	require.Equal(t, len(keys), i)
}

func TestIdInternalizer_LookupDoesNotAllocate(t *testing.T) {
	s, err := NewIdInternalizer(identifier.TagValue(1), 0)
	require.NoError(t, err)
	key := []byte("de305d54-75b4-431b-adb2-eb6b9e546013")
	_, _, err = s.GetId(key)
	require.NoError(t, err)
	allocs := testing.AllocsPerRun(1000, func() {
		_, _, _ = s.GetId(key) // existing key: the string(key) map lookup must not allocate
	})
	require.Zero(t, allocs, "GetId of an existing key must not allocate")
}

func TestIdInternalizedGenerator_CreatesWorkingGenerator(t *testing.T) {
	f := NewIdInternalizedGenerator()
	gen, err := f.Create(identifier.TagValue(9), 128)
	require.NoError(t, err)

	id, fresh, err := gen.GetId([]byte("k"))
	require.NoError(t, err)
	require.True(t, fresh)
	require.True(t, id.IsValid())
	require.EqualValues(t, 9, id.GetTag().GetValue())

	require.NoError(t, f.Close())
	require.NoError(t, gen.Release())

	// Usable after Release, and the mapping is retained.
	id2, fresh2, err := gen.GetId([]byte("k"))
	require.NoError(t, err)
	require.False(t, fresh2)
	require.Equal(t, id, id2)
}

// TestIdInternalizer_PropertyConsistency mirrors the identifier package's own
// randomized round-trip test: internalize a stream of random keys and check the
// core invariants against a shadow map.
func TestIdInternalizer_PropertyConsistency(t *testing.T) {
	r := rand.New(rand.NewPCG(0x1234, 0x5678))
	const tagVal = identifier.TagValue(7)
	s, err := NewIdInternalizer(tagVal, 16)
	require.NoError(t, err)

	keyPool := make([][]byte, 0, 64)
	for range 64 {
		k := make([]byte, 1+r.IntN(20))
		for j := range k {
			k[j] = byte(r.Uint64())
		}
		keyPool = append(keyPool, k)
	}

	shadow := make(map[string]identifier.TaggedId)
	n := 20000
	if testing.Short() {
		n = 2000
	}
	for range n {
		k := keyPool[r.IntN(len(keyPool))]
		id, fresh, err := s.GetId(k)
		require.NoError(t, err)
		require.True(t, id.IsValid())
		require.EqualValues(t, tagVal, id.GetTag().GetValue())

		if prev, seen := shadow[string(k)]; seen {
			require.False(t, fresh)
			require.Equal(t, prev, id)
		} else {
			require.True(t, fresh)
			shadow[string(k)] = id
		}

		got, found := s.Resolve(id)
		require.True(t, found)
		require.Equal(t, string(k), got)
	}
	require.Equal(t, len(shadow), s.Len())
}

func TestIdInternalizer_AppendIds(t *testing.T) {
	const tagVal = identifier.TagValue(5)
	gen, err := NewIdInternalizer(tagVal, 8)
	require.NoError(t, err)

	seq := []string{"a", "b", "a", "c", "b"}
	var keys identgen.KeysColumn
	for _, k := range seq {
		keys = keys.AppendKey([]byte(k))
	}

	ids, fresh, err := gen.AppendIds(nil, keys, make([]bool, 0))
	require.NoError(t, err)
	require.Len(t, ids, 5)
	require.Len(t, fresh, 5)

	// Alignment + dedup: a,b,a,c,b.
	require.Equal(t, ids[0], ids[2])
	require.Equal(t, ids[1], ids[4])
	require.NotEqual(t, ids[0], ids[1])
	require.NotEqual(t, ids[0], ids[3])
	require.Equal(t, []bool{true, true, false, true, false}, fresh)
	for _, id := range ids {
		require.True(t, id.IsValid())
		require.EqualValues(t, tagVal, id.GetTag().GetValue())
	}
	require.Equal(t, 3, gen.Len())

	// The batch agrees with single GetId for the same keys.
	for i, k := range seq {
		gotID, _, err := gen.GetId([]byte(k))
		require.NoError(t, err)
		require.Equal(t, ids[i], gotID)
	}
}

func TestIdInternalizer_AppendIds_NilFreshAndDstReuse(t *testing.T) {
	gen, err := NewIdInternalizer(identifier.TagValue(1), 0)
	require.NoError(t, err)
	keys := identgen.KeysColumn{}.AppendKey([]byte("x")).AppendKey([]byte("y"))

	// nil fresh -> flags not tracked.
	ids, freshOut, err := gen.AppendIds(nil, keys, nil)
	require.NoError(t, err)
	require.Nil(t, freshOut)
	require.Len(t, ids, 2)

	// dst reuse: results are appended after existing elements.
	dst := []identifier.TaggedId{0xdead}
	out, _, err := gen.AppendIds(dst, keys, nil)
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.EqualValues(t, 0xdead, out[0])
	require.Equal(t, ids[0], out[1])
	require.Equal(t, ids[1], out[2])
}

func TestIdInternalizer_AppendIds_RejectsEmptyKeyAtomically(t *testing.T) {
	gen, err := NewIdInternalizer(identifier.TagValue(1), 0)
	require.NoError(t, err)
	keys := identgen.KeysColumn{}.AppendKey([]byte("ok")).AppendKey([]byte("")).AppendKey([]byte("also"))

	out, _, err := gen.AppendIds([]identifier.TaggedId{}, keys, nil)
	require.ErrorIs(t, err, identgen.ErrEmptyNaturalKey)
	require.Empty(t, out, "dst returned unmodified")
	require.Equal(t, 0, gen.Len(), "nothing minted on a bad batch")
}

func BenchmarkIdInternalizer_GetId(b *testing.B) {
	key := []byte("de305d54-75b4-431b-adb2-eb6b9e546013")

	// hit: an already-internalized key; the map lookup must not allocate.
	b.Run("hit", func(b *testing.B) {
		gen, err := NewIdInternalizer(identifier.TagValue(1), 16)
		require.NoError(b, err)
		_, _, _ = gen.GetId(key) // prime
		b.ReportAllocs()
		for b.Loop() {
			_, _, _ = gen.GetId(key)
		}
	})

	// miss: a fresh key each time; the working set is capped so memory stays bounded.
	b.Run("miss", func(b *testing.B) {
		const workingSet = 1 << 20
		gen, err := NewIdInternalizer(identifier.TagValue(1), workingSet)
		require.NoError(b, err)
		buf := make([]byte, 8)
		var i uint64
		b.ReportAllocs()
		for b.Loop() {
			if gen.Len() >= workingSet {
				b.StopTimer()
				gen, _ = NewIdInternalizer(identifier.TagValue(1), workingSet)
				b.StartTimer()
			}
			binary.LittleEndian.PutUint64(buf, i)
			i++
			_, _, _ = gen.GetId(buf)
		}
	})
}
