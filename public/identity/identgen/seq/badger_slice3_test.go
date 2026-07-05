package seq

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// TestCreate_SecondGeneratorForTagErrors: a second sequential generator on
// one tag would lease a disjoint id block and interleave the stream; Create
// must refuse it (ADR-0106 SD6).
func TestCreate_SecondGeneratorForTagErrors(t *testing.T) {
	genFac, err := NewBadgerIdSequenceGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	gen, err := genFac.Create(identifier.TagValue(3), 8)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	_, err = genFac.Create(identifier.TagValue(3), 8)
	require.ErrorIs(t, err, identgen.ErrTagInUse)

	require.NoError(t, gen.Release())
	_, err = genFac.Create(identifier.TagValue(3), 8)
	require.ErrorIs(t, err, identgen.ErrTagInUse, "Release keeps the generator usable, the slot stays held")
}

// TestExhaustion_MaxWidthTag: the widest uint32 tag holds 2^17-1 bodies; the
// stream must end with ErrIdSpaceExhausted, not a wrapped or corrupt id.
func TestExhaustion_MaxWidthTag(t *testing.T) {
	if testing.Short() {
		t.Skip("draws a 131071-id stream")
	}
	genFac, err := NewBadgerIdSequenceGenerator(t.TempDir())
	require.NoError(t, err)
	defer func() { _ = genFac.Close() }()

	const capacity = 1<<17 - 1
	gen, err := genFac.Create(identifier.TagValue(math.MaxUint32), 1<<18)
	require.NoError(t, err)
	defer func() { _ = gen.Release() }()

	var last identifier.UntaggedId
	for i := 0; i < capacity; i++ {
		u, _, err := gen.GetUntaggedId(nil)
		require.NoError(t, err)
		last = u
	}
	require.EqualValues(t, capacity, last, "the stream must end exactly at the body capacity")
	_, _, err = gen.GetUntaggedId(nil)
	require.ErrorIs(t, err, identgen.ErrIdSpaceExhausted)
}
