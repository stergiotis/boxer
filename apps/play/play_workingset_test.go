package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// newWorkingsetTestApp is a PlayApp with no client, already past its
// baseline frame — the shape the dirty tests want, since the first
// syncWorkingsetDirty only anchors.
func newWorkingsetTestApp(t *testing.T, sql string) (inst *PlayApp) {
	t.Helper()
	inst = NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), sql, nil)
	inst.syncWorkingsetDirty()
	require.False(t, inst.WorkingsetDirty(), "the seeded state is not an edit")
	return
}

// Dirty tracking (ADR-0148 §SD4): what counts as a person acting.

func TestWorkingsetDirty_BaselineIsNotAnEdit(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	// Repeated frames with nothing happening keep it clean.
	inst.syncWorkingsetDirty()
	inst.syncWorkingsetDirty()
	assert.False(t, inst.WorkingsetDirty())
}

func TestWorkingsetDirty_SqlEdit(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	inst.sql = "SELECT 2"
	inst.syncWorkingsetDirty()
	assert.True(t, inst.WorkingsetDirty())
}

func TestWorkingsetDirty_BandsEdit(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	inst.timelineBandsSql = "SELECT 'band'"
	inst.syncWorkingsetDirty()
	assert.True(t, inst.WorkingsetDirty())
}

func TestWorkingsetDirty_LiveToggle(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	inst.liveMain = true
	inst.syncWorkingsetDirty()
	assert.True(t, inst.WorkingsetDirty())
}

func TestWorkingsetDirty_TabRaise(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	require.NoError(t, inst.ActivateTab("table"))
	inst.syncWorkingsetDirty()
	assert.True(t, inst.WorkingsetDirty())
}

// newRunnableWorkingsetApp is newWorkingsetTestApp against a stub server,
// so executeRun can actually dispatch.
func newRunnableWorkingsetApp(t *testing.T) (inst *PlayApp) {
	t.Helper()
	srv, _ := captureServer(t)
	t.Cleanup(srv.Close)
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	inst = NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	t.Cleanup(inst.graph.close)
	inst.frameSig = inst.graph.signals()
	inst.syncWorkingsetDirty()
	require.False(t, inst.WorkingsetDirty())
	return
}

func TestWorkingsetDirty_ManualRun(t *testing.T) {
	// A manual Run is intent even when the buffer never changed — the old
	// persist-on-Run anchor, kept.
	inst := newRunnableWorkingsetApp(t)
	inst.executeRun(false, false)
	assert.True(t, inst.WorkingsetDirty())
}

func TestWorkingsetDirty_AutoRunIsNotIntent(t *testing.T) {
	inst := newRunnableWorkingsetApp(t)
	inst.executeRun(true, false)
	inst.syncWorkingsetDirty()
	assert.False(t, inst.WorkingsetDirty(), "a live-toggle re-run is signal churn, not intent")
}

func TestWorkingsetDirty_BreakerTripIsNotIntent(t *testing.T) {
	// The circuit breaker unchecking Live is the one machine write to a
	// composed field; it re-anchors instead of reading as an edit.
	inst := newWorkingsetTestApp(t, "SELECT 1")
	inst.liveMain = true
	inst.syncWorkingsetDirty()
	inst.workingsetDirty = false // ignore the (genuine) toggle above
	inst.workingsetSeen.Live = true

	inst.liveMain = false
	inst.rebaseWorkingsetLive(false)
	inst.syncWorkingsetDirty()
	assert.False(t, inst.WorkingsetDirty())

	// …and the user re-checking it — the resume gesture — is intent again.
	inst.liveMain = true
	inst.syncWorkingsetDirty()
	assert.True(t, inst.WorkingsetDirty())
}

// Compose (ADR-0148 §SD2/§SD8): the launch that would reproduce this
// window.

func TestComposeLaunch_CarriesTheAuthoredState(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	inst.sql = "SELECT 2"
	inst.timelineBandsSql = "SELECT 'band'"
	inst.liveMain = true
	require.NoError(t, inst.ActivateTab("timeline"))

	cfg := inst.ComposeLaunch()
	assert.Equal(t, "SELECT 2", cfg.Sql)
	assert.Equal(t, "SELECT 'band'", cfg.BandsSql)
	assert.True(t, cfg.Live)
	assert.Equal(t, "timeline", cfg.Tab)
	assert.False(t, cfg.AutoRun, "restoration is not re-execution")
	assert.Empty(t, cfg.Endpoint, "the endpoint belongs to the run, not the state")
	assert.False(t, cfg.At.IsZero())
}

func TestComposeLaunch_NoTabRaisedMeansDefaultTab(t *testing.T) {
	inst := newWorkingsetTestApp(t, "SELECT 1")
	assert.Empty(t, inst.ComposeLaunch().Tab)
}

func TestComposeWorkingset_CleanWindowComposesNothing(t *testing.T) {
	// The poisoned-inheritance case, as a regression test: a window opened
	// on someone else's config and closed untouched must leave the stored
	// record alone.
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'seeded'"})
	inst, err := mountLauncher(t, cfg, mapStorage{})
	require.NoError(t, err)
	inst.inner.syncWorkingsetDirty()

	out, dirty, err := inst.ComposeWorkingset()
	require.NoError(t, err)
	assert.False(t, dirty)
	assert.Empty(t, out)
}

func TestComposeWorkingset_DirtyWindowEncodesItsState(t *testing.T) {
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'seeded'"})
	inst, err := mountLauncher(t, cfg, mapStorage{})
	require.NoError(t, err)
	inst.inner.syncWorkingsetDirty()
	inst.inner.sql = "SELECT 'edited'"
	inst.inner.syncWorkingsetDirty()

	out, dirty, err := inst.ComposeWorkingset()
	require.NoError(t, err)
	require.True(t, dirty)
	require.NotEmpty(t, out)
	got, err := buscodec.Decode[launchcfg.PlayLaunch](out)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'edited'", got.Sql)
	assert.False(t, got.AutoRun)
}

func TestComposeWorkingset_AfterUnmountIsQuiet(t *testing.T) {
	// The host composes before Unmount; anything that asks afterwards gets
	// the honest answer rather than a nil deref.
	inst := &PlayLauncher{}
	out, dirty, err := inst.ComposeWorkingset()
	require.NoError(t, err)
	assert.False(t, dirty)
	assert.Empty(t, out)
}

// Mount precedence (ADR-0148 §SD5): caller config > env > restored
// record > legacy key > default.

func TestMount_RestoredRecordLosesToEnv(t *testing.T) {
	t.Setenv("BOXER_PLAY_SQL", "SELECT 'env'")
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'restored'"})

	inst, err := mountLauncherReason(t, cfg, mapStorage{}, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'env'", inst.inner.sql)
}

func TestMount_CallerConfigBeatsEnv(t *testing.T) {
	t.Setenv("BOXER_PLAY_SQL", "SELECT 'env'")
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'caller'"})

	inst, err := mountLauncherReason(t, cfg, mapStorage{}, app.LaunchReasonCaller)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'caller'", inst.inner.sql)
}

func TestMount_RestoredRecordBeatsLegacyKey(t *testing.T) {
	store := mapStorage{persistKeyLastSql: []byte("SELECT 'legacy'")}
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'restored'"})

	inst, err := mountLauncherReason(t, cfg, store, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'restored'", inst.inner.sql)
}

func TestMount_PlainOpenStillReadsTheLegacyKey(t *testing.T) {
	store := mapStorage{persistKeyLastSql: []byte("SELECT 'legacy'")}

	inst, err := mountLauncherReason(t, nil, store, app.LaunchReasonPlain)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'legacy'", inst.inner.sql)
}

func TestMount_PlainOpenWithNothingStoredUsesTheDefault(t *testing.T) {
	inst, err := mountLauncherReason(t, nil, mapStorage{}, app.LaunchReasonPlain)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM boxer.facts", inst.inner.sql)
}

func TestMount_RestoredEmptyBandsAreNotResurrected(t *testing.T) {
	// The restore tier applies BandsSql unconditionally: the record says
	// the user cleared the bands buffer, and neither the legacy key nor
	// the caller-tier "only when non-empty" rule may bring it back.
	store := mapStorage{persistKeyTimelineBandsSql: []byte("SELECT 'old bands'")}
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'restored'", BandsSql: ""})

	inst, err := mountLauncherReason(t, cfg, store, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.Empty(t, inst.inner.timelineBandsSql)
}

func TestMount_RestoredBandsBeatTheLegacyKey(t *testing.T) {
	store := mapStorage{persistKeyTimelineBandsSql: []byte("SELECT 'old bands'")}
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql: "SELECT 'restored'", BandsSql: "SELECT 'new bands'",
	})

	inst, err := mountLauncherReason(t, cfg, store, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'new bands'", inst.inner.timelineBandsSql)
}

func TestMount_BandsEnvOverrideBeatsARestoredRecord(t *testing.T) {
	t.Setenv("BOXER_PLAY_TIMELINE_BANDS_SQL", "SELECT 'env bands'")
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql: "SELECT 'restored'", BandsSql: "SELECT 'restored bands'",
	})

	inst, err := mountLauncherReason(t, cfg, mapStorage{}, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'env bands'", inst.inner.timelineBandsSql)
}

func TestMount_RestoredRecordDoesNotDriveAutoRun(t *testing.T) {
	// A record composes AutoRun false by construction, so the restore tier
	// stays out of the AutoRun decision rather than overriding
	// BOXER_PLAY_AUTORUN with a meaningless false. Shown with a config
	// that says true, which only the caller tier acts on (the env handle
	// caches its first read, so it cannot be moved from inside a test).
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{Sql: "SELECT 'x'", AutoRun: true})

	caller, err := mountLauncherReason(t, cfg, mapStorage{}, app.LaunchReasonCaller)
	require.NoError(t, err)
	assert.True(t, caller.inner.AutoRun, "a caller states its complete opening intent")

	restored, err := mountLauncherReason(t, cfg, mapStorage{}, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.False(t, restored.inner.AutoRun, "the restore tier leaves whatever the env decided")
}

func TestMount_RestoredLiveAndTabApplyAsComposed(t *testing.T) {
	cfg := encodePlayLaunch(t, launchcfg.PlayLaunch{
		Sql: "SELECT 'restored'", Live: true, Tab: "table",
	})

	inst, err := mountLauncherReason(t, cfg, mapStorage{}, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.True(t, inst.inner.liveMain)
	wantDock, ok := inst.inner.tabs.dockIDForSlug("table")
	require.True(t, ok)
	assert.Equal(t, wantDock, inst.inner.pendingDockActivate)
	// Seeding is not an edit: the window is clean until someone acts.
	inst.inner.syncWorkingsetDirty()
	assert.False(t, inst.inner.WorkingsetDirty())
}

func TestWorkingset_ComposeRestoreRoundTrip(t *testing.T) {
	// The whole loop with the host's own steps in between: compose what a
	// closing window holds, put it through the gate the host applies
	// before storing, store it, read it back, and open with it. The
	// kindcheck step is the one that would catch play composing bytes the
	// host would refuse — a save that fails there is silent by design.
	first := newWorkingsetTestApp(t, "SELECT 1")
	first.sql = "SELECT 'authored'"
	first.timelineBandsSql = "" // the user cleared the bands buffer
	first.liveMain = true
	first.syncWorkingsetDirty()
	require.True(t, first.WorkingsetDirty())

	cfg, err := buscodec.Encode(first.ComposeLaunch())
	require.NoError(t, err)
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, kindcheck.Check(m.LaunchKind, cfg),
		"composed bytes must satisfy the host's boundary check")
	require.LessOrEqual(t, len(cfg), 64<<10, "…and the host's size cap")

	facts := factsstore.NewInMemoryFactsStore()
	_, err = facts.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: m.Id, Name: windowhost.WorkingsetDefaultName,
		Kind: m.LaunchKind, Config: cfg, TileKey: 1, Reason: "user-close",
	})
	require.NoError(t, err)
	stored, kind, found, err := facts.LatestWorkingset(m.Id, windowhost.WorkingsetDefaultName)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, m.LaunchKind, kind)

	// A stale legacy bands value sits underneath; the restored record's
	// empty buffer must win.
	store := mapStorage{persistKeyTimelineBandsSql: []byte("SELECT 'old bands'")}
	second, err := mountLauncherReason(t, stored, store, app.LaunchReasonRestore)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'authored'", second.inner.sql)
	assert.Empty(t, second.inner.timelineBandsSql)
	assert.True(t, second.inner.liveMain)

	// A restored window is clean until someone acts in it.
	second.inner.syncWorkingsetDirty()
	assert.False(t, second.inner.WorkingsetDirty())
	out, dirty, err := second.ComposeWorkingset()
	require.NoError(t, err)
	assert.False(t, dirty)
	assert.Empty(t, out)
}
