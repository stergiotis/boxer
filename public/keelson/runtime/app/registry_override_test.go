package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func overrideFixture(t *testing.T) (reg *Registry) {
	t.Helper()
	reg = NewRegistry()
	for _, id := range []AppIdT{"test.a", "test.b"} {
		m := Manifest{Id: id, Version: "0.1.0", Display: string(id), Summary: "fixture",
			Surface: SurfaceWindowed, Topics: []TopicT{TopicData}, Keywords: []string{"k"}}
		require.NoError(t, reg.RegisterFactory(m, func() (a AppI, err error) { return }))
	}
	return
}

func TestOverrideManifests_ReachesEveryManifestReader(t *testing.T) {
	reg := overrideFixture(t)
	before := reg.Manifests()
	display, grade := "Renamed", MaturitySupported
	require.NoError(t, reg.OverrideManifests(map[AppIdT]ManifestOverride{
		"test.a": {Display: &display, Maturity: &grade},
	}))

	m, ok := reg.LookupManifest("test.a")
	require.True(t, ok)
	assert.Equal(t, "Renamed", m.Display)
	assert.Equal(t, MaturitySupported, m.Maturity)
	assert.Equal(t, "Renamed", reg.LaunchableManifests()[0].Display)
	assert.Equal(t, "Renamed", reg.Registrations()[0].Manifest.Display)
	assert.Equal(t, []string{"display", "maturity"}, reg.Registrations()[0].Overridden)
	assert.Equal(t, []string{"display", "maturity"}, reg.Overridden("test.a"))
	assert.Nil(t, reg.Overridden("test.b"))
	assert.Equal(t, "test.a", before[0].Display, "a snapshot handed out before keeps its values")
}

func TestOverrideManifests_SameValueIsNoOverride(t *testing.T) {
	reg := overrideFixture(t)
	same := "test.a"
	require.NoError(t, reg.OverrideManifests(map[AppIdT]ManifestOverride{"test.a": {Display: &same}}))
	assert.Nil(t, reg.Overridden("test.a"))
}

// One invalid override refuses the whole batch.
func TestOverrideManifests_InvalidChangesNothing(t *testing.T) {
	reg := overrideFixture(t)
	display, empty := "Renamed", ""
	err := reg.OverrideManifests(map[AppIdT]ManifestOverride{
		"test.a": {Display: &display},
		"test.b": {Summary: &empty},
	})
	require.Error(t, err)
	m, _ := reg.LookupManifest("test.a")
	assert.Equal(t, "test.a", m.Display)

	unknown := []TopicT{"no-such-topic"}
	require.Error(t, reg.OverrideManifests(map[AppIdT]ManifestOverride{"test.a": {Topics: &unknown}}))
	require.Error(t, reg.OverrideManifests(map[AppIdT]ManifestOverride{"test.missing": {Display: &display}}))
}

func TestOverrideManifests_KeepsTheLaunchLimit(t *testing.T) {
	reg := overrideFixture(t)
	reg.LimitLaunches([]AppIdT{"test.b"})
	display := "Renamed"
	require.NoError(t, reg.OverrideManifests(map[AppIdT]ManifestOverride{"test.b": {Display: &display}}))
	ms := reg.LaunchableManifests()
	require.Len(t, ms, 1)
	assert.Equal(t, "Renamed", ms[0].Display)
	assert.False(t, reg.Launchable("test.a"))
}
