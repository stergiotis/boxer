package launcher

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// recordingHost records which verb the launcher chose.
type recordingHost struct {
	calls []string
}

var _ HostI = (*recordingHost)(nil)

func (inst *recordingHost) OpenOrRaiseApp(appId app.AppIdT) (err error) {
	inst.calls = append(inst.calls, "raise:"+string(appId))
	return
}
func (inst *recordingHost) OpenNewApp(appId app.AppIdT) (err error) {
	inst.calls = append(inst.calls, "new:"+string(appId))
	return
}
func (inst *recordingHost) OpenAppIds() (ids []app.AppIdT) { return }

// The default action follows the registration (ADR-0214 §Updates
// 2026-11-12): a factory app opens a new window, a singleton raises.
func TestLaunchFollowsTheRegistration(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(&stubApp{m: mkKindManifest("test.single", "test.single", app.KindApp)}))
	require.NoError(t, reg.RegisterFactory(mkKindManifest("test.factory", "test.factory", app.KindApp), func() (a app.AppI, err error) {
		a = &stubApp{m: mkKindManifest("test.factory", "test.factory", app.KindApp)}
		return
	}))
	host := &recordingHost{}
	inst := New(reg, host, nil, zerolog.Nop())

	inst.launch("test.factory")
	inst.launch("test.single")
	inst.launch("test.unknown")
	assert.Equal(t, []string{"new:test.factory", "raise:test.single", "raise:test.unknown"}, host.calls,
		"an id the registry does not hold falls back to open-or-raise, which reports it")

	assert.Equal(t, launchVerbOpen, inst.launchVerb("test.factory", false))
	assert.Equal(t, launchVerbNewWindow, inst.launchVerb("test.factory", true))
	assert.Equal(t, launchVerbOpen, inst.launchVerb("test.single", false))
	assert.Equal(t, launchVerbRaise, inst.launchVerb("test.single", true))
}

func TestRegistrySingleton(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(&stubApp{m: mkKindManifest("test.single", "test.single", app.KindApp)}))
	require.NoError(t, reg.RegisterFactory(mkKindManifest("test.factory", "test.factory", app.KindApp), func() (a app.AppI, err error) {
		a = &stubApp{m: mkKindManifest("test.factory", "test.factory", app.KindApp)}
		return
	}))
	s, ok := reg.Singleton("test.single")
	assert.True(t, ok)
	assert.True(t, s)
	s, ok = reg.Singleton("test.factory")
	assert.True(t, ok)
	assert.False(t, s)
	_, ok = reg.Singleton("test.unknown")
	assert.False(t, ok)
}

// A launcher opened when none was starts from an empty search box and keeps
// the facet filters; a second window opened beside the first keeps the query
// both share.
func TestFirstLauncherWindowStartsWithAnEmptyQuery(t *testing.T) {
	inst := newTestInst()
	inst.searchText = "log viewer"
	inst.topicFilter = topicFilterT(0).toggledAt(0)

	first := NewApp(inst)
	require.NoError(t, first.Mount(nil))
	inst.consumeFreshQuery()
	assert.Equal(t, "", inst.searchText, "a fresh launch clears the last search")
	assert.Equal(t, topicFilterT(0).toggledAt(0), inst.topicFilter, "the chips carry over")

	inst.searchText = "play"
	second := NewApp(inst)
	require.NoError(t, second.Mount(nil))
	inst.consumeFreshQuery()
	assert.Equal(t, "play", inst.searchText, "a window beside an open one shares its query")

	require.NoError(t, second.Unmount(nil))
	require.NoError(t, first.Unmount(nil))
	require.NoError(t, NewApp(inst).Mount(nil))
	inst.consumeFreshQuery()
	assert.Equal(t, "", inst.searchText, "closed and reopened, the box is empty again")
}
