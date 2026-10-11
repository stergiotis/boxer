package chat

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
)

// The window labels confined content by the declaration its host handed
// it, not by a second read of the environment, and every coordinator it
// makes carries that tier; without one it keeps the environment's.
func TestTheChatLabelsByTheHostsDeclaration(t *testing.T) {
	fallback := newApp()
	assert.Equal(t, fallbackConfinedTier(), fallback.confinedTier)

	host := cell.Defaults()
	host.ConfinedTier = agentrun.TierRed
	require.NotEqual(t, fallbackConfinedTier(), host.ConfinedTier)
	mc := app.NewStaticMountContext(ManifestId, zerolog.Nop(), nil, nil, nil)
	mc.SetMountValue(cell.MountKey{}, &host)
	inst := newApp()
	require.NoError(t, inst.Mount(mc))
	t.Cleanup(func() { _ = inst.Unmount(mc) })
	assert.Equal(t, agentrun.TierRed, inst.confinedTier)
	assert.Equal(t, agentrun.TierRed, inst.coord.confinedTier)
	assert.Equal(t, agentrun.TierRed, inst.newCoordinator("c2").confinedTier)
	assert.Equal(t, agentrun.TierRed, outcomeLabel(false, true, inst.coord.confinedTier).Tier)
}

// The run is pinned to the confined tier its labels were computed under:
// a resume under a host that maps confined content to another tier is
// read-only, a resume under the same tier continues, and the calls keep
// naming the profile's own fingerprint.
func TestAResumeUnderAnotherConfinedTierIsReadOnly(t *testing.T) {
	store := agentrun.NewMemStore()
	under := func(tier agentrun.TierE) *App {
		inst := appOver(t, &fakeModel{}, store)
		inst.confinedTier, inst.coord.confinedTier = tier, tier
		return inst
	}
	first := under(agentrun.TierRed)
	first.keep = true
	require.True(t, first.startTurn("q1"))
	drainUntil(t, first)
	settleTitle(t, first)
	id := first.conv.id
	run, _, _ := conversationRun(store, id)
	assert.Equal(t, runFingerprint(profileNow().Fingerprint(), agentrun.TierRed), run.Fingerprint)
	ref, _ := first.coord.profileRef()
	assert.Equal(t, defaultProfileId+"@"+clip(profileNow().Fingerprint(), 12), ref)

	same := under(agentrun.TierRed)
	same.resumeRun(id)
	require.Eventually(t, func() bool { same.drain(); return same.conv.id == id }, 5*time.Second, 5*time.Millisecond)
	assert.False(t, same.conv.stranded)
	ref, _ = same.coord.profileRef()
	assert.Equal(t, defaultProfileId+"@"+clip(profileNow().Fingerprint(), 12), ref, "resume names the profile, not the run")
	settleTitle(t, same)

	other := under(agentrun.TierAmber)
	other.resumeRun(id)
	require.Eventually(t, func() bool { other.drain(); return other.conv.id == id }, 5*time.Second, 5*time.Millisecond)
	assert.True(t, other.conv.stranded)
	assert.False(t, other.startTurn("q2"))
}
