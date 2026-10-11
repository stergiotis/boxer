package cell

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// An app reads the declaration its host handed it; a host that hands none
// is told apart.
func TestAnAppReadsTheHostsDeclaration(t *testing.T) {
	mc := app.NewStaticMountContext("x", zerolog.Nop(), nil, nil, nil)
	_, ok := OfMount(mc)
	assert.False(t, ok)

	host := Defaults()
	host.ConfinedTier = agentrun.TierRed
	mc.SetMountValue(MountKey{}, &host)
	d, ok := OfMount(mc)
	require.True(t, ok)
	assert.Equal(t, agentrun.TierRed, d.ConfinedTier)

	mc.SetMountValue(MountKey{}, (*Declaration)(nil))
	_, ok = OfMount(mc)
	assert.False(t, ok, "a nil declaration is none")
}
