package launcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// noHistoryStore stands for the in-memory facts store: a writer that offers no
// history capability. It implements nothing but the type assertion's negative.
type noHistoryStore struct{ factsstore.FactsStoreI }

// historyStore is a store that answers, for the positive path.
type historyStore struct {
	factsstore.FactsStoreI
	stats    []factsstore.AppLaunchStat
	err      error
	halfLife time.Duration
	calls    int
}

func (inst *historyStore) AppLaunchStats(ctx context.Context, halfLife time.Duration, limit uint32) (stats []factsstore.AppLaunchStat, err error) {
	inst.calls++
	inst.halfLife = halfLife
	return inst.stats, inst.err
}

func newTestInst() (inst *Inst) {
	inst = New(app.NewRegistry(), nil, nil, zerolog.Nop())
	return
}

// TestBindHistory_AbsentCapabilityIsNotAnError is §SD7's whole reason for
// making this an optional capability. A run without ClickHouse gets the
// in-memory store, which cannot answer — and the right behaviour there is
// authored-metadata ordering, not a degraded mode and not a warning every
// boot.
func TestBindHistory_AbsentCapabilityIsNotAnError(t *testing.T) {
	inst := newTestInst()
	installed := inst.BindHistory(context.Background(), noHistoryStore{})
	assert.False(t, installed)
	assert.Nil(t, inst.rank, "no ranking is installed")
	assert.Nil(t, inst.recentFn, "and no recents")
	assert.Equal(t, 0, inst.rank.bonus("anything"), "the nil rankFn still answers")
}

// TestBindHistory_ReadFailureFallsBack: a store that offers the capability and
// then fails it is a live server that went away. The launcher must still open.
func TestBindHistory_ReadFailureFallsBack(t *testing.T) {
	inst := newTestInst()
	store := &historyStore{err: errors.New("server gone")}
	assert.False(t, inst.BindHistory(context.Background(), store))
	assert.Nil(t, inst.rank)
}

// TestBindHistory_EmptyTrailInstallsNothing: a first run has the capability and
// no rows. An all-zero ranking is indistinguishable from no ranking, so the
// simpler state is the one to be in.
func TestBindHistory_EmptyTrailInstallsNothing(t *testing.T) {
	inst := newTestInst()
	store := &historyStore{stats: []factsstore.AppLaunchStat{}}
	assert.False(t, inst.BindHistory(context.Background(), store))
	assert.Nil(t, inst.recentFn)
}

// TestBindHistory_InstallsBothOrders is the positive path, and it pins that
// the half-life the store is asked for is the resolved knob rather than a
// hardcoded constant.
func TestBindHistory_InstallsBothOrders(t *testing.T) {
	HalfLife.SetForTest(t, "72h")
	inst := newTestInst()
	store := &historyStore{stats: []factsstore.AppLaunchStat{
		mkStat("a", 10, 0, 100),
		mkStat("b", 1, 30, 0.5),
	}}
	require.True(t, inst.BindHistory(context.Background(), store))
	assert.Equal(t, 1, store.calls, "read once at bind, not per frame")
	assert.Equal(t, 72*time.Hour, store.halfLife)
	require.NotNil(t, inst.rank)
	require.NotNil(t, inst.recentFn)
	assert.Greater(t, inst.rank.bonus("a"), inst.rank.bonus("b"))
	assert.Equal(t, 0, inst.rank.bonus("never-opened"))
	assert.Equal(t, []app.AppIdT{"a", "b"}, inst.recentFn())
}

// TestRecentManifests_DropsUnknownIdsAndItself covers what the menu must not
// show: an applet the store replaced since the history read, and a door back
// to the window the click came from.
func TestRecentManifests_DropsUnknownIdsAndItself(t *testing.T) {
	reg := app.NewRegistry()
	keep := mkTopicManifest("keep", "Keep", app.TopicData)
	require.NoError(t, reg.RegisterFactory(keep, func() (a app.AppI, err error) {
		a = &stubApp{m: keep}
		return
	}))
	inst := New(reg, nil, nil, zerolog.Nop())
	inst.recentFn = func() (ids []app.AppIdT) {
		ids = []app.AppIdT{ManifestId, "gone", "keep"}
		return
	}
	got := inst.recentManifests()
	require.Len(t, got, 1)
	assert.Equal(t, app.AppIdT("keep"), got[0].Id)
}

// TestRecentManifests_BoundedByMenuMax keeps the menu a shortcut: a recents
// list that needs scrolling is the cascade ADR-0214 §SD2 replaced.
func TestRecentManifests_BoundedByMenuMax(t *testing.T) {
	reg := app.NewRegistry()
	ids := make([]app.AppIdT, 0, menuRecentsMax+5)
	for i := 0; i < menuRecentsMax+5; i++ {
		id := app.AppIdT("app-" + string(rune('a'+i)))
		m := mkTopicManifest(string(id), "App "+string(rune('A'+i)), app.TopicData)
		require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) {
			a = &stubApp{m: m}
			return
		}))
		ids = append(ids, id)
	}
	inst := New(reg, nil, nil, zerolog.Nop())
	inst.recentFn = func() (out []app.AppIdT) { out = ids; return }
	assert.Len(t, inst.recentManifests(), menuRecentsMax)
}

type stubApp struct{ m app.Manifest }

func (inst *stubApp) Manifest() (m app.Manifest)                { m = inst.m; return }
func (inst *stubApp) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *stubApp) Frame(ctx app.FrameContextI) (err error)   { return }
func (inst *stubApp) Unmount(ctx app.MountContextI) (err error) { return }
