package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func manifestIds(ms []Manifest) (ids []AppIdT) {
	for _, m := range ms {
		ids = append(ids, m.Id)
	}
	return
}

func TestRegistry_NoLimit_EverythingLaunchable(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newTestApp(t, "org.test.a")))
	assert.True(t, reg.Launchable("org.test.a"))
	assert.Equal(t, []AppIdT{"org.test.a"}, manifestIds(reg.LaunchableManifests()))
}

func TestRegistry_LimitLaunches_RefusesUnselected(t *testing.T) {
	reg := NewRegistry()
	for _, id := range []AppIdT{"org.test.a", "org.test.b", "org.test.c"} {
		require.NoError(t, reg.Register(newTestApp(t, id)))
	}
	reg.LimitLaunches([]AppIdT{"org.test.b"})
	assert.False(t, reg.Launchable("org.test.a"))
	assert.True(t, reg.Launchable("org.test.b"))
	assert.False(t, reg.Launchable("org.test.c"))
	assert.Equal(t, []AppIdT{"org.test.b"}, manifestIds(reg.LaunchableManifests()))
	assert.Len(t, reg.Manifests(), 3, "the limit hides nothing from the registry itself")
}

// ADR-0272 §SD2: the limit names the apps the process booted with; an app
// registered afterwards is launchable.
func TestRegistry_LimitLaunches_LaterRegistrationLaunchable(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(newTestApp(t, "org.test.a")))
	reg.LimitLaunches(nil)
	assert.False(t, reg.Launchable("org.test.a"))
	assert.Empty(t, reg.LaunchableManifests())

	require.NoError(t, reg.Register(newTestApp(t, "org.test.late")))
	assert.True(t, reg.Launchable("org.test.late"))
	assert.Equal(t, []AppIdT{"org.test.late"}, manifestIds(reg.LaunchableManifests()))
}
