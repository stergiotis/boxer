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
	assert.Equal(t, defaultRounds, p.Rounds)
	assert.Len(t, p.Tools, len(coordinatorTools))
	assert.Equal(t, []string{"context"}, p.Inject)
	assert.Equal(t, []string{"round", "compact", "last-round"}, p.Configure)
	assert.Equal(t, "sequential", p.ToolMode)

	twin := p
	twin.Id = "twin"
	require.NoError(t, twin.Validate())
	assert.Equal(t, p.Fingerprint(), twin.Fingerprint(), "the id is not in the fingerprint")
	moved := p
	moved.Rounds = 16
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
	p.Rounds = 1
	assert.Error(t, p.Validate())
	p = defaultProfile()
	p.PromptBook = "other"
	assert.Error(t, p.Validate())
}
