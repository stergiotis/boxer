package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The one registered profile validates and is what the selector names; a
// second profile with the same values under another name fingerprints
// alike, and a changed constant moves the fingerprint.
func TestTheDefaultProfileValidatesAndFingerprints(t *testing.T) {
	p := profileNow()
	require.NoError(t, p.Validate())
	assert.Equal(t, defaultProfileId, p.Id)
	assert.Equal(t, []string{defaultProfileId}, profiles().Ids())
	assert.Equal(t, defaultRoundsLo, p.RoundsMin)
	assert.Equal(t, defaultRoundsHi, p.RoundsMax)
	assert.Len(t, p.Tools, len(coordinatorTools))
	assert.Equal(t, []string{"context"}, p.Inject)
	assert.Equal(t, []string{"round", "compact", "last-round"}, p.Configure)
	assert.Equal(t, "sequential", p.ToolMode)

	twin := p
	twin.Id = "twin"
	require.NoError(t, twin.Validate())
	assert.Equal(t, p.Fingerprint(), twin.Fingerprint(), "the id is not in the fingerprint")
	moved := p
	moved.RoundsMax = 16
	assert.NotEqual(t, p.Fingerprint(), moved.Fingerprint())
	assert.Equal(t, p.Fingerprint(), profileNow().Fingerprint(), "stable across renders")
}

func TestAProfileThatDoesNotMatchTheBuildIsRefused(t *testing.T) {
	p := defaultProfile()
	p.Tools = p.Tools[1:]
	assert.Error(t, p.Validate(), "a tool set that is not the registry's")
	p = defaultProfile()
	p.Configure = []string{"last-round", "round"}
	assert.Error(t, p.Validate(), "a chain in another order")
	p = defaultProfile()
	p.ToolMode = "parallel"
	assert.Error(t, p.Validate(), "a tool mode this build does not have")
	p = defaultProfile()
	p.RoundsMin = 1
	assert.Error(t, p.Validate())
	p = defaultProfile()
	p.PixelsMax = "all"
	assert.Error(t, p.Validate())
	p = defaultProfile()
	p.PromptBook = "other"
	assert.Error(t, p.Validate())
}

// setProfileForTest makes p the profile profileNow returns until t ends.
func setProfileForTest(t testing.TB, p ProfileT) {
	t.Helper()
	prev := profileForTest.Swap(&p)
	t.Cleanup(func() { profileForTest.Store(prev) })
}

// A limit is read from the profile, so the profile in force is what binds
// and what the fingerprint names; a starting value is not in it.
func TestTheLimitsAreTheProfiles(t *testing.T) {
	p := defaultProfile()
	before := p.Fingerprint()
	p.RoundsMin, p.RoundsMax = 6, 8
	p.ImageBudgetCount = 3
	setProfileForTest(t, p)
	lo, hi := roundRange()
	assert.Equal(t, 6, lo)
	assert.Equal(t, 8, hi)
	assert.Equal(t, 8, clampRounds(50))
	assert.Equal(t, 3, imageLimitsNow().count)
	assert.NotEqual(t, before, profileNow().Fingerprint())
}
