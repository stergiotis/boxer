package tagmint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
)

// Values for the space tests, below the ones tagmint_test.go claims.
const (
	tvSpaceShared = identifier.TagValue(3524560)
	tvSpaceOther  = identifier.TagValue(3524561)
)

func TestOneValueIsClaimableOncePerSpace(t *testing.T) {
	a := tagmint.MustClaimSpace("tagmintTestSpaceA")
	b := tagmint.MustClaimSpace("tagmintTestSpaceB")

	x, err := tagmint.ClaimIn(a, "tagmintTestSpaceX", tvSpaceShared, 1)
	require.NoError(t, err)
	y, err := tagmint.ClaimIn(b, "tagmintTestSpaceY", tvSpaceShared, 1)
	require.NoError(t, err, "a value in another space is another id")
	assert.Equal(t, a, x.Space())
	assert.Equal(t, b, y.Space())
	assert.Equal(t, "tagmintTestSpaceA", x.Space().Name())
	assert.Contains(t, x.Space().Origin(), "tagmint_space_test.go")

	_, err = tagmint.ClaimIn(a, "tagmintTestSpaceZ", tvSpaceShared, 1)
	require.Error(t, err, "within one space the value is taken")
	f := ebtest.Fields(t, err)
	assert.Equal(t, "tagmintTestSpaceX", f["claimant"])
	assert.Equal(t, "tagmintTestSpaceA", f["space"])
	assert.Contains(t, f["claimedAt"], "tagmint_space_test.go")

	_, err = tagmint.ClaimIn(a, "tagmintTestSpaceX", tvSpaceOther, 1)
	require.Error(t, err, "names are unique across spaces")
	assert.Contains(t, err.Error(), "name is already claimed")
	_, err = tagmint.ClaimIn(b, "tagmintTestSpaceX", tvSpaceOther, 1)
	require.Error(t, err, "also from another space")

	w, err := tagmint.Claim("tagmintTestSpaceW", tvSpaceShared, 1)
	require.NoError(t, err, "the default space is a space of its own")
	assert.True(t, w.Space().IsDefault())
	assert.Equal(t, "", w.Space().Name())
	assert.Equal(t, tagmint.Space{}, w.Space())

	_, err = tagmint.ClaimIn(tagmint.Space{}, "tagmintTestSpaceWAgain", tvSpaceShared, 1)
	require.Error(t, err, "the zero Space is the default space Claim uses")
}

func TestClaimSpaceRefusesADuplicateAndAMalformedName(t *testing.T) {
	_, err := tagmint.ClaimSpace("tagmintTestSpaceDup")
	require.NoError(t, err)
	_, err = tagmint.ClaimSpace("tagmintTestSpaceDup")
	require.Error(t, err)
	assert.Contains(t, ebtest.Fields(t, err)["claimedAt"], "tagmint_space_test.go")

	for _, name := range []string{"", "with space", "with\ttab", "with\nnewline"} {
		_, err = tagmint.ClaimSpace(name)
		assert.Error(t, err, "%q", name)
	}
}

// The fit check does not depend on the space: a value refused in the default
// space is refused in a named one.
func TestClaimInKeepsTheFitCheck(t *testing.T) {
	sp := tagmint.MustClaimSpace("tagmintTestSpaceFit")
	cl, err := fibonacci.WidthClassOf(tagmint.VocabularyTagWidth)
	require.NoError(t, err)

	_, err = tagmint.ClaimIn(sp, "tagmintTestSpaceTooMany", cl.TagValueMinIncl+7, cl.MaxBodyIncl+1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fewer")
	_, err = tagmint.ClaimIn(sp, "tagmintTestSpaceZeroValue", 0, 1)
	require.Error(t, err)
	_, err = tagmint.ClaimIn(sp, "tagmintTestSpaceZeroIds", tvSpaceOther, 0)
	require.Error(t, err)
	_, err = tagmint.ClaimIn(sp, "", tvSpaceOther, 1)
	require.Error(t, err)

	// A value that fits is claimed, whatever the default space holds under
	// the same number.
	c, err := tagmint.ClaimIn(sp, "tagmintTestSpaceFits", cl.TagValueMinIncl+7, cl.MaxBodyIncl)
	require.NoError(t, err)
	assert.Equal(t, sp, c.Space())
}

func TestIterateClaimsYieldsTheSpace(t *testing.T) {
	sp := tagmint.MustClaimSpace("tagmintTestSpaceIter")
	c := tagmint.MustClaimIn(sp, "tagmintTestSpaceIterated", tvSpaceOther, 1)

	type key struct {
		space string
		value identifier.TagValue
	}
	seen := map[key]string{}
	var seenOurs bool
	for cl := range tagmint.IterateClaims() {
		k := key{space: cl.Space().Name(), value: cl.Value()}
		if prev, dup := seen[k]; dup {
			t.Fatalf("(space %q, tag value %d) yielded twice: %q and %q", k.space, k.value.Value(), prev, cl.Name())
		}
		seen[k] = cl.Name()
		if cl.Name() == c.Name() {
			seenOurs = true
			assert.Equal(t, sp, cl.Space())
		}
	}
	assert.True(t, seenOurs)
}

func TestZeroTokenReportsTheDefaultSpace(t *testing.T) {
	var unclaimed tagmint.ClaimedTagValue
	assert.True(t, unclaimed.Space().IsDefault())
}
