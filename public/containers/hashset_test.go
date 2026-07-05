package containers

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashSet_AddHasRemove(t *testing.T) {
	hs := NewHashSet[string](4)
	require.True(t, hs.IsEmpty())
	require.Equal(t, 0, hs.Size())
	require.False(t, hs.Has("a"))

	hs.Add("a")
	hs.Add("a") // idempotent
	hs.Add("b")
	require.True(t, hs.Has("a"))
	require.True(t, hs.Has("b"))
	require.False(t, hs.Has("c"))
	require.Equal(t, 2, hs.Size())
	require.False(t, hs.IsEmpty())

	hs.Remove("a")
	require.False(t, hs.Has("a"))
	require.Equal(t, 1, hs.Size())
	hs.Remove("missing") // no-op
	require.Equal(t, 1, hs.Size())
}

func TestHashSet_AddExRemoveEx(t *testing.T) {
	hs := NewHashSet[int](4)
	require.False(t, hs.AddEx(1), "fresh insert reports existed=false")
	require.True(t, hs.AddEx(1), "re-insert reports existed=true")
	require.True(t, hs.RemoveEx(1), "removing a member reports had=true")
	require.False(t, hs.RemoveEx(1), "removing a non-member reports had=false")
}

func TestHashSet_AddMany_ReturnsNewlyAdded(t *testing.T) {
	hs := NewHashSet[int](4)
	hs.Add(1)
	hs.Add(2)
	added := hs.AddMany(slices.Values([]int{1, 2, 3, 3, 4}))
	require.Equal(t, 2, added, "only 3 and 4 are new; duplicates in the seq count once")
	require.Equal(t, 4, hs.Size())

	require.Equal(t, 0, hs.AddMany(slices.Values([]int{1, 2})), "all present → 0")
	require.Equal(t, 0, hs.AddMany(slices.Values([]int{})), "empty seq → 0")
}

func TestHashSet_AddExMany(t *testing.T) {
	hs := NewHashSet[int](4)
	hs.Add(1)
	existing, nonExisting := hs.AddExMany(slices.Values([]int{1, 2, 2, 3}))
	require.Equal(t, 2, existing, "1 pre-existing + second 2 counts as existing")
	require.Equal(t, 2, nonExisting, "2 and 3 are new")
	require.Equal(t, 3, hs.Size())
}

func TestHashSet_IterateAll(t *testing.T) {
	hs := NewHashSet[int](4)
	for i := range 5 {
		hs.Add(i)
	}
	collected := slices.Collect(hs.IterateAll())
	slices.Sort(collected)
	require.Equal(t, []int{0, 1, 2, 3, 4}, collected)

	n := 0
	for range hs.IterateAll() {
		n++
		if n == 2 {
			break
		}
	}
	require.Equal(t, 2, n, "early termination honoured")
}

func TestHashSet_Clear(t *testing.T) {
	hs := NewHashSet[string](4)
	hs.Add("a")
	hs.Add("b")
	hs.Clear()
	require.True(t, hs.IsEmpty())
	require.False(t, hs.Has("a"))
	hs.Add("c")
	require.Equal(t, 1, hs.Size(), "set usable after Clear")
}

func TestHashSet_SliceAndSliceEx(t *testing.T) {
	hs := NewHashSet[int](4)
	hs.Add(2)
	hs.Add(1)

	s := hs.Slice()
	require.ElementsMatch(t, []int{1, 2}, s)

	prefix := []int{99}
	out := hs.SliceEx(prefix)
	require.Len(t, out, 3)
	require.Equal(t, 99, out[0], "SliceEx appends after the existing prefix")
	require.ElementsMatch(t, []int{1, 2}, out[1:])

	empty := NewHashSet[int](0)
	require.Empty(t, empty.Slice())
}

func TestHashSet_UnionMod(t *testing.T) {
	a := NewHashSet[int](4)
	a.Add(1)
	a.Add(2)
	b := NewHashSet[int](4)
	b.Add(2)
	b.Add(3)

	a.UnionMod(b)
	require.ElementsMatch(t, []int{1, 2, 3}, a.Slice())
	require.ElementsMatch(t, []int{2, 3}, b.Slice(), "argument unchanged")

	a.UnionMod(a) // self-union is a no-op
	require.Equal(t, 3, a.Size())
}

func TestHashSet_DifferenceMod(t *testing.T) {
	a := NewHashSet[int](4)
	a.Add(1)
	a.Add(2)
	a.Add(3)
	b := NewHashSet[int](4)
	b.Add(2)
	b.Add(9)

	a.DifferenceMod(b)
	require.ElementsMatch(t, []int{1, 3}, a.Slice())
	require.ElementsMatch(t, []int{2, 9}, b.Slice(), "argument unchanged")

	a.DifferenceMod(a) // self-difference empties the set
	require.True(t, a.IsEmpty())
}

func TestHashSet_IntersectMod(t *testing.T) {
	a := NewHashSet[int](4)
	a.Add(1)
	a.Add(2)
	a.Add(3)
	b := NewHashSet[int](4)
	b.Add(2)
	b.Add(3)
	b.Add(4)

	a.IntersectMod(b)
	require.ElementsMatch(t, []int{2, 3}, a.Slice())
	require.ElementsMatch(t, []int{2, 3, 4}, b.Slice(), "argument unchanged")

	a.IntersectMod(a) // self-intersection is a no-op
	require.ElementsMatch(t, []int{2, 3}, a.Slice())
}

func TestHashSet_Equal(t *testing.T) {
	a := NewHashSet[string](4)
	b := NewHashSet[string](8) // capacity does not matter
	require.True(t, a.Equal(b), "two empty sets are equal")

	a.Add("x")
	require.False(t, a.Equal(b))
	b.Add("x")
	require.True(t, a.Equal(b))
	b.Add("y")
	require.False(t, a.Equal(b), "size mismatch")
	a.Add("z")
	require.False(t, a.Equal(b), "same size, different elements")
}

func TestHashSet_Clone_Independence(t *testing.T) {
	a := NewHashSet[int](4)
	a.Add(1)
	a.Add(2)
	c := a.Clone()
	require.True(t, a.Equal(c))

	c.Add(3)
	a.Remove(1)
	require.ElementsMatch(t, []int{2}, a.Slice())
	require.ElementsMatch(t, []int{1, 2, 3}, c.Slice())

	// Non-destructive algebra via Clone + *Mod.
	x := NewHashSet[int](4)
	x.Add(2)
	x.Add(3)
	inter := c.Clone()
	inter.IntersectMod(x)
	require.ElementsMatch(t, []int{2, 3}, inter.Slice())
	require.ElementsMatch(t, []int{1, 2, 3}, c.Slice(), "source of the clone unchanged")
}

// TestDifferentialFuzz_HashSetAgainstMap reconciles a HashSet against a
// map[T]struct{} oracle under a mixed operation stream, including the
// set algebra against a second set/oracle pair.
func TestDifferentialFuzz_HashSetAgainstMap(t *testing.T) {
	const (
		steps        = 3000
		checkEvery   = 20
		keyspaceSize = 24
	)
	rnd := rand.New(rand.NewPCG(0xFEEDFACE, 0xC0DEC0DE))

	hs := NewHashSet[string](8)
	oracle := map[string]struct{}{}
	other := NewHashSet[string](8)
	otherOracle := map[string]struct{}{}

	key := func() string { return fmt.Sprintf("k%02d", rnd.IntN(keyspaceSize)) }

	check := func(step int) {
		t.Helper()
		require.Equal(t, len(oracle), hs.Size(), "size mismatch at step %d", step)
		require.Equal(t, len(oracle) == 0, hs.IsEmpty(), "IsEmpty mismatch at step %d", step)
		collected := slices.Collect(hs.IterateAll())
		require.Len(t, collected, len(oracle), "iteration count mismatch at step %d", step)
		for _, k := range collected {
			_, ok := oracle[k]
			require.True(t, ok, "extra element %q at step %d", k, step)
		}
		for k := range oracle {
			require.True(t, hs.Has(k), "missing element %q at step %d", k, step)
		}
	}

	for step := range steps {
		k := key()
		switch rnd.IntN(12) {
		case 0, 1:
			hs.Add(k)
			oracle[k] = struct{}{}
		case 2:
			_, wantExisted := oracle[k]
			require.Equal(t, wantExisted, hs.AddEx(k), "AddEx flag at step %d", step)
			oracle[k] = struct{}{}
		case 3:
			hs.Remove(k)
			delete(oracle, k)
		case 4:
			_, wantHad := oracle[k]
			require.Equal(t, wantHad, hs.RemoveEx(k), "RemoveEx flag at step %d", step)
			delete(oracle, k)
		case 5:
			vals := []string{key(), key(), key()}
			wantAdded := 0
			seen := map[string]struct{}{}
			for _, v := range vals {
				if _, ok := oracle[v]; !ok {
					if _, dup := seen[v]; !dup {
						wantAdded++
					}
				}
				seen[v] = struct{}{}
			}
			require.Equal(t, wantAdded, hs.AddMany(slices.Values(vals)), "AddMany count at step %d", step)
			for _, v := range vals {
				oracle[v] = struct{}{}
			}
		case 6:
			other.Add(k)
			otherOracle[k] = struct{}{}
		case 7:
			hs.UnionMod(other)
			maps.Copy(oracle, otherOracle)
		case 8:
			hs.DifferenceMod(other)
			for v := range otherOracle {
				delete(oracle, v)
			}
		case 9:
			hs.IntersectMod(other)
			for v := range oracle {
				if _, ok := otherOracle[v]; !ok {
					delete(oracle, v)
				}
			}
		case 10:
			// Equality probe against a clone and against a rebuilt set.
			cl := hs.Clone()
			require.True(t, hs.Equal(cl), "clone must equal source at step %d", step)
			rebuilt := NewHashSet[string](len(oracle))
			for v := range oracle {
				rebuilt.Add(v)
			}
			require.True(t, hs.Equal(rebuilt), "oracle-rebuilt set must equal at step %d", step)
		case 11:
			if rnd.IntN(30) == 0 {
				hs.Clear()
				clear(oracle)
			}
		}
		if step%checkEvery == 0 {
			check(step)
		}
	}
	check(steps)
}
