package tagmint_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
)

// The claims live in package state, so tests that claim must not reuse a
// value another test claimed. These sit at the top of the width-32 class,
// below the reserved runtime mint, where no vocabulary claims.
const (
	tvTestA = identifier.TagValue(3524570)
	tvTestB = identifier.TagValue(3524571)
	tvTestC = identifier.TagValue(3524572)
	tvTestD = identifier.TagValue(3524573)
)

func TestClaimReturnsAUsableToken(t *testing.T) {
	c, err := tagmint.Claim("tagmintTestA", tvTestA, 1000)
	require.NoError(t, err)
	assert.True(t, c.IsValid())
	assert.Equal(t, tvTestA, c.Value())
	assert.Equal(t, tvTestA.GetTag(), c.Tag())
	assert.Equal(t, "tagmintTestA", c.Name())
	assert.EqualValues(t, 1000, c.MaxExpectedIds())
	assert.Contains(t, c.Origin(), "tagmint_test.go", "the claim points at its declaration, not at the mint")

	found, has := tagmint.Lookup("tagmintTestA")
	require.True(t, has)
	assert.Equal(t, tvTestA, found.Value())
}

// The zero value is what a caller outside the package can build, and it is the
// thing every constructor demanding a claim has to reject. Without this the
// token would be a comment rather than a fence.
func TestZeroTokenIsNotAClaim(t *testing.T) {
	var unclaimed tagmint.ClaimedTagValue
	assert.False(t, unclaimed.IsValid())
	assert.EqualValues(t, 0, unclaimed.Value())
	assert.Equal(t, "", unclaimed.Name())
	assert.EqualValues(t, 0, unclaimed.MaxExpectedIds())
	assert.Equal(t, "", unclaimed.Origin())
}

func TestClaimRefusesADuplicateValue(t *testing.T) {
	_, err := tagmint.Claim("tagmintTestB", tvTestB, 10)
	require.NoError(t, err)
	_, err = tagmint.Claim("tagmintTestBAgain", tvTestB, 10)
	require.Error(t, err, "ids under one tag value are one id space")
	assert.Contains(t, err.Error(), "tagmintTestB", "the error names the first claimant")
	assert.Contains(t, err.Error(), "tagmint_test.go", "and where it claimed")
}

func TestClaimRefusesADuplicateName(t *testing.T) {
	_, err := tagmint.Claim("tagmintTestC", tvTestC, 10)
	require.NoError(t, err)
	_, err = tagmint.Claim("tagmintTestC", tvTestD, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already claimed")
}

// Fit: the claimed value's class must hold the ids the family says it needs.
// A vocabulary outgrowing its class is then a refusal at init rather than an
// AddTag panic once the bodies reach the tag's bits.
func TestClaimRefusesAValueTooNarrowForTheDeclaredCardinality(t *testing.T) {
	cl, err := fibonacci.WidthClassOf(tagmint.VocabularyTagWidth)
	require.NoError(t, err)

	_, err = tagmint.Claim("tagmintTestTooMany", cl.TagValueMinIncl+7, cl.MaxBodyIncl+1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fewer than")

	// One below the class ceiling fits, and claims the value for good.
	_, err = tagmint.Claim("tagmintTestFits", cl.TagValueMinIncl+7, cl.MaxBodyIncl)
	require.NoError(t, err, "the refusal above must not have consumed the value")
}

func TestClaimRefusesTheInvalidSentinelAndTheEmptyFamily(t *testing.T) {
	_, err := tagmint.Claim("tagmintTestZeroValue", 0, 10)
	require.Error(t, err)
	_, err = tagmint.Claim("tagmintTestZeroIds", tvTestD, 0)
	require.Error(t, err)
	_, err = tagmint.Claim("", tvTestD, 10)
	require.Error(t, err)
}

// The reserved runtime tag is claimed by the package itself, so a vocabulary
// reaching for it is refused by the ordinary uniqueness rule.
func TestRuntimeMintIsReservedByAClaim(t *testing.T) {
	assert.True(t, tagmint.RuntimeMint.IsValid())
	assert.Equal(t, tagmint.RuntimeMintTagValue, tagmint.RuntimeMint.Value())

	_, err := tagmint.Claim("tagmintTestStealsTheReservation", tagmint.RuntimeMintTagValue, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtimeMint")
}

// The two constants are the split this decision rests on: 32 bits of tag, 32
// of body, with the reservation at the far end of that same class. A change
// here re-keys every vocabulary, so it is pinned rather than derived at the
// call sites.
func TestVocabularyClassIsTheThirtyTwoBitSplit(t *testing.T) {
	cl, err := fibonacci.WidthClassOf(tagmint.VocabularyTagWidth)
	require.NoError(t, err)
	assert.EqualValues(t, 32, cl.Width)
	assert.EqualValues(t, 1<<32-1, cl.MaxBodyIncl, "32 body bits")
	assert.EqualValues(t, 2178309, cl.TagValueMinIncl.Value())
	assert.EqualValues(t, 3524577, cl.TagValueMaxIncl.Value())
	assert.EqualValues(t, 1346269, cl.TagValueCount)

	assert.Equal(t, cl.TagValueMaxIncl, tagmint.RuntimeMintTagValue,
		"the reservation is the class maximum")
	assert.Equal(t, tagmint.VocabularyTagWidth, tagmint.RuntimeMintTagValue.GetTag().GetTagWidth())
}

func TestIterateClaimsCoversWhatWasClaimed(t *testing.T) {
	c, err := tagmint.Claim("tagmintTestIterated", tvTestD, 10)
	require.NoError(t, err)

	var seenRuntimeMint, seenOurs bool
	values := map[identifier.TagValue]string{}
	for cl := range tagmint.IterateClaims() {
		require.True(t, cl.IsValid())
		if prev, dup := values[cl.Value()]; dup {
			t.Fatalf("tag value %d yielded twice: %q and %q", cl.Value().Value(), prev, cl.Name())
		}
		values[cl.Value()] = cl.Name()
		switch cl.Name() {
		case "runtimeMint":
			seenRuntimeMint = true
		case c.Name():
			seenOurs = true
		}
	}
	assert.True(t, seenRuntimeMint)
	assert.True(t, seenOurs)
}
