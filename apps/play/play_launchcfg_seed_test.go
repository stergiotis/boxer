package play

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// mapStorage is a StorageI over a plain map, standing in for the persist
// layer so the seeding tests can pin the persisted tier.
type mapStorage map[string][]byte

var _ app.StorageI = mapStorage{}

func (m mapStorage) Get(key string) (value []byte, found bool, err error) {
	value, found = m[key]
	return
}
func (m mapStorage) Set(key string, value []byte) (err error) {
	m[key] = value
	return
}
func (m mapStorage) Delete(key string) (err error) {
	delete(m, key)
	return
}

func encodePlayLaunch(t *testing.T, lc launchcfg.PlayLaunch) (b []byte) {
	t.Helper()
	lc.At = time.Unix(0, 1_700_000_000_000_000_000).UTC()
	b, err := buscodec.Encode(lc)
	require.NoError(t, err)
	return
}

// mountLauncher runs PlayLauncher.Mount with the given launch-config
// bytes and storage contents, returning the launcher for inner-state
// assertions. The caller owns Unmount (registered as cleanup here).
func mountLauncher(t *testing.T, cfg []byte, storage app.StorageI) (inst *PlayLauncher, err error) {
	t.Helper()
	inst = &PlayLauncher{}
	mc := app.NewStaticMountContext("test.play.launch", zerolog.Nop(), storage, nil, nil)
	mc.SetLaunchConfig(cfg)
	err = inst.Mount(mc)
	if err == nil {
		t.Cleanup(func() { _ = inst.Unmount(mc) })
	}
	return
}

func TestMount_SeedingPriority_ConfigBeatsEnvBeatsPersisted(t *testing.T) {
	// All three tiers present: the config must win, and the loser tiers
	// must not leak into any knob.
	t.Setenv("BOXER_PLAY_SQL", "SELECT 'env'")
	store := mapStorage{persistKeyLastSql: []byte("SELECT 'persisted'")}
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql:      "SELECT 'config'",
		AutoRun:  true,
		Live:     true,
		BandsSql: "SELECT 'bands'",
		Tab:      "table",
	})

	inst, err := mountLauncher(t, cfg, store)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'config'", inst.inner.sql)
	assert.True(t, inst.inner.AutoRun, "config AutoRun replaces the env tier")
	assert.True(t, inst.inner.liveMain)
	assert.Equal(t, "SELECT 'bands'", inst.inner.timelineBandsSql)
	wantDock, ok := inst.inner.tabs.dockIDForSlug("table")
	require.True(t, ok)
	assert.Equal(t, wantDock, inst.inner.pendingDockActivate)
}

func TestMount_SeedingPriority_EnvBeatsPersisted(t *testing.T) {
	t.Setenv("BOXER_PLAY_SQL", "SELECT 'env'")
	store := mapStorage{persistKeyLastSql: []byte("SELECT 'persisted'")}

	inst, err := mountLauncher(t, nil, store)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'env'", inst.inner.sql)
}

func TestMount_SeedingPriority_PersistedBeatsDefault(t *testing.T) {
	store := mapStorage{persistKeyLastSql: []byte("SELECT 'persisted'")}

	inst, err := mountLauncher(t, nil, store)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'persisted'", inst.inner.sql)
}

func TestMount_GarbageLaunchConfigIsMountError(t *testing.T) {
	// The host's kind check gates this in production; the app-side
	// contract (§SD4: decode failure = visible mount error, no silent
	// fallback) must hold regardless.
	inst, err := mountLauncher(t, []byte("not a playLaunch payload"), mapStorage{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode launch config")
	assert.Nil(t, inst.inner, "no half-mounted inner app on decode failure")
}

func TestMount_UnknownTabIsWarningNotMountError(t *testing.T) {
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql: "SELECT 1",
		Tab: "no-such-tab",
	})
	inst, err := mountLauncher(t, cfg, mapStorage{})
	require.NoError(t, err, "an unknown tab id degrades, it does not fail the mount")
	assert.Equal(t, "SELECT 1", inst.inner.sql)
}
