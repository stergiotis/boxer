package hostboot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
)

// A caller's declaration replaces the environment's: the host enforces it
// with what the binary registers added, and the caller's value is left as
// it was handed in.
func TestBootTakesTheCallersDeclaration(t *testing.T) {
	supplied := cell.Defaults()
	supplied.ConfinedTier = agentrun.TierRed
	egress := len(supplied.Egress)
	opts := minimalOptions(t, newTestRegistry(t))
	opts.Cell = &supplied
	rt, err := Boot(context.Background(), opts)
	require.NoError(t, err)
	defer rt.Close()
	require.NotNil(t, rt.Cell)
	assert.NotSame(t, &supplied, rt.Cell)
	assert.Equal(t, agentrun.TierRed, rt.Cell.ConfinedTier)
	assert.Len(t, supplied.Egress, egress, "the caller's declaration is not changed")
}
