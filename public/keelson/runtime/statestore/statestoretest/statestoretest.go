// Package statestoretest is the contract every statestore.StoreI
// implementation answers: the in-memory statestore.Memory and the durable
// persist.StoreBackend run the same cases, so the fallback a host uses with
// ClickHouse down cannot drift from the backend it uses with it up.
//
// The cases were the facts stores' before ADR-0105's Update of 2026-08-15
// moved both kinds off `boxer.facts`, kept case for case so the move is
// checked against the behaviour it replaced.
package statestoretest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
)

// OpenFunc returns a fresh, empty store for one case.
type OpenFunc func(t *testing.T) statestore.StoreI

// Run executes every case against stores open returns.
func Run(t *testing.T, open OpenFunc) {
	t.Run("Workingset/LatestWins", func(t *testing.T) { workingsetLatestWins(t, open(t)) })
	t.Run("Workingset/Missing", func(t *testing.T) { workingsetMissing(t, open(t)) })
	t.Run("Workingset/DeleteTombstones", func(t *testing.T) { workingsetDeleteTombstones(t, open(t)) })
	t.Run("Workingset/NameAndAppIsolation", func(t *testing.T) { workingsetIsolation(t, open(t)) })
	t.Run("Workingset/NestedAppIds", func(t *testing.T) { workingsetNestedAppIds(t, open(t)) })
	t.Run("Workingset/ListLatestPerKey", func(t *testing.T) { workingsetListLatestPerKey(t, open(t)) })
	t.Run("Workingset/ListEmpty", func(t *testing.T) { workingsetListEmpty(t, open(t)) })
	t.Run("Workingset/ListTombstoneExcludesKey", func(t *testing.T) { workingsetListTombstone(t, open(t)) })
	t.Run("Workingset/DefensiveCopy", func(t *testing.T) { workingsetDefensiveCopy(t, open(t)) })
	t.Run("ColumnWidth/LatestPerKey", func(t *testing.T) { colWidthLatestPerKey(t, open(t)) })
	t.Run("ColumnWidth/TierIsPartOfIdentity", func(t *testing.T) { colWidthTierIdentity(t, open(t)) })
	t.Run("ColumnWidth/DeleteThenWrite", func(t *testing.T) { colWidthDeleteThenWrite(t, open(t)) })
	t.Run("ColumnWidth/DeleteAbsentKey", func(t *testing.T) { colWidthDeleteAbsent(t, open(t)) })
	t.Run("ColumnWidth/EmptyForUnknownApp", func(t *testing.T) { colWidthUnknownApp(t, open(t)) })
	t.Run("ColumnWidth/NestedAppIds", func(t *testing.T) { colWidthNestedAppIds(t, open(t)) })
	t.Run("ColumnWidth/RoundTripsEveryField", func(t *testing.T) { colWidthRoundTrip(t, open(t)) })
}

// past is the base timestamp of a case's writes: a minute ago, so a delete
// — which an implementation stamps with the current time — is newer than
// every write before it, as it is in use. A write stamped after "now" would
// outrank a later delete in a store that orders by timestamp while losing
// to it in one that orders by insertion, and the suite would be testing
// its own clock rather than the store.
func past() time.Time { return time.Now().UTC().Add(-time.Minute) }

func ws(appId string, name string, kind string, cfg string, ts time.Time) statestore.WorkingsetRow {
	return statestore.WorkingsetRow{AppId: app.AppIdT(appId), Name: name, Kind: kind, Config: []byte(cfg), Ts: ts}
}

func workingsetLatestWins(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteWorkingset(ws("play", "default", "playLaunch", "v1", t0)))
	require.NoError(t, s.WriteWorkingset(ws("play", "default", "playLaunch", "v2", t0.Add(time.Second))))
	cfg, kind, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "v2", string(cfg))
	assert.Equal(t, "playLaunch", kind, "the kind is its own column, read back as written")
}

func workingsetMissing(t *testing.T, s statestore.StoreI) {
	cfg, kind, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err, "a missing record is found=false, not an error")
	assert.False(t, found)
	assert.Nil(t, cfg)
	assert.Empty(t, kind)
}

func workingsetDeleteTombstones(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteWorkingset(ws("play", "default", "k", "v1", t0)))
	require.NoError(t, s.DeleteWorkingset("play", "default"))
	_, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.False(t, found, "a deleted record must read as absent, not fall back to the write before it")

	require.NoError(t, s.WriteWorkingset(ws("play", "default", "k", "v3", time.Now().UTC())))
	cfg, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	require.True(t, found, "a write after the delete is live again")
	assert.Equal(t, "v3", string(cfg))
}

func workingsetIsolation(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteWorkingset(ws("play", "default", "k", "play-default", t0)))
	require.NoError(t, s.WriteWorkingset(ws("play", "other", "k", "play-other", t0)))
	require.NoError(t, s.WriteWorkingset(ws("tally", "default", "k", "tally-default", t0)))
	for _, c := range []struct{ app, name, want string }{
		{"play", "default", "play-default"},
		{"play", "other", "play-other"},
		{"tally", "default", "tally-default"},
	} {
		cfg, _, found, err := s.LatestWorkingset(app.AppIdT(c.app), c.name)
		require.NoError(t, err)
		require.True(t, found, "%s/%s", c.app, c.name)
		assert.Equal(t, c.want, string(cfg))
	}
}

// App ids are import paths and nest (every applet sits under the sqlapplet
// app's id), so a store keying on a joined string must keep (app "a/b",
// name "c") apart from (app "a", name "b/c").
func workingsetNestedAppIds(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteWorkingset(ws("x/a", "b", "k", "nested-app", t0)))
	require.NoError(t, s.WriteWorkingset(ws("x", "a/b", "k", "nested-name", t0)))
	cfg, _, found, err := s.LatestWorkingset("x/a", "b")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "nested-app", string(cfg))
	cfg, _, found, err = s.LatestWorkingset("x", "a/b")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "nested-name", string(cfg))
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func workingsetListLatestPerKey(t *testing.T, s statestore.StoreI) {
	t0 := past()
	row := ws("play", "default", "playLaunch", "v1", t0)
	row.RunId, row.TileKey, row.Reason = "run-1", 7, "user-close"
	require.NoError(t, s.WriteWorkingset(row))
	row = ws("play", "default", "playLaunch", "v2", t0.Add(time.Second))
	row.RunId, row.TileKey, row.Reason = "run-2", 9, "shutdown"
	require.NoError(t, s.WriteWorkingset(row))
	require.NoError(t, s.WriteWorkingset(ws("imztop", "default", "imztopLaunch", "i1", t0)))

	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 2, "one row per (app, name), not per write")
	assert.Equal(t, app.AppIdT("imztop"), rows[0].AppId, "ordered by app id")
	assert.Equal(t, app.AppIdT("play"), rows[1].AppId)
	got := rows[1]
	assert.Equal(t, "v2", string(got.Config))
	assert.Equal(t, "default", got.Name)
	assert.Equal(t, "playLaunch", got.Kind)
	assert.Equal(t, "run-2", got.RunId, "provenance comes from the winning row")
	assert.Equal(t, uint64(9), got.TileKey)
	assert.Equal(t, "shutdown", got.Reason)
	assert.WithinDuration(t, t0.Add(time.Second), got.Ts, time.Millisecond, "Ts is the winning row's write time")
}

func workingsetListEmpty(t *testing.T, s statestore.StoreI) {
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func workingsetListTombstone(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteWorkingset(ws("play", "default", "k", "v1", t0)))
	require.NoError(t, s.WriteWorkingset(ws("tally", "default", "k", "t1", t0)))
	require.NoError(t, s.DeleteWorkingset("play", "default"))
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 1, "a key whose newest row is a tombstone is absent from the list")
	assert.Equal(t, app.AppIdT("tally"), rows[0].AppId)
}

func workingsetDefensiveCopy(t *testing.T, s statestore.StoreI) {
	buf := []byte("original")
	row := ws("play", "default", "k", "", time.Now().UTC())
	row.Config = buf
	require.NoError(t, s.WriteWorkingset(row))
	copy(buf, "CLOBBERD")
	cfg, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "original", string(cfg), "the store must not alias the caller's buffer")
	cfg[0] = 'X'
	again, _, _, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.Equal(t, "original", string(again), "nor hand out its own")
}

func cw(appId string, tier string, scope string, key string, points float64, ts time.Time) statestore.ColumnWidthRow {
	return statestore.ColumnWidthRow{AppId: app.AppIdT(appId), Tier: tier, Scope: scope, ColumnKey: key, Points: points, FontSize: 12, Ts: ts}
}

func colWidthLatestPerKey(t *testing.T, s statestore.StoreI) {
	t0 := past()
	for _, row := range []statestore.ColumnWidthRow{
		cw("play", statestore.ColWidthTierInstance, "attrs", "k1", 100, t0),
		cw("play", statestore.ColWidthTierInstance, "attrs", "k1", 140, t0.Add(time.Second)),
		cw("play", statestore.ColWidthTierColumn, "", "k1", 90, t0),
		cw("imztop", statestore.ColWidthTierColumn, "", "k1", 50, t0),
	} {
		require.NoError(t, s.WriteColumnWidth(row))
	}
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 2, "one row per key, not per write")
	// SortColumnWidths orders by tier: "column" precedes "instance".
	assert.Equal(t, statestore.ColWidthTierColumn, rows[0].Tier)
	assert.Equal(t, 90.0, rows[0].Points)
	assert.Equal(t, statestore.ColWidthTierInstance, rows[1].Tier)
	assert.Equal(t, 140.0, rows[1].Points, "the later write wins")
}

// The tier is part of the identity: an instance-tier and a column-tier
// entry for the same column key are different overrides.
func colWidthTierIdentity(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteColumnWidth(cw("play", statestore.ColWidthTierInstance, "t", "k", 10, t0)))
	require.NoError(t, s.WriteColumnWidth(cw("play", statestore.ColWidthTierColumn, "", "k", 20, t0)))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

// A cleared override stays cleared: clearing must not let an older
// surviving write show through, and a write after the clear is live.
func colWidthDeleteThenWrite(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteColumnWidth(cw("play", statestore.ColWidthTierColumn, "", "k", 10, t0)))
	require.NoError(t, s.WriteColumnWidth(cw("play", statestore.ColWidthTierColumn, "", "k", 20, t0.Add(time.Millisecond))))
	require.NoError(t, s.DeleteColumnWidth("play", statestore.ColWidthTierColumn, "", "k"))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows, "a cleared override must not fall back to an older write")

	require.NoError(t, s.WriteColumnWidth(cw("play", statestore.ColWidthTierColumn, "", "k", 30, time.Now().UTC())))
	rows, err = s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 30.0, rows[0].Points)
}

func colWidthDeleteAbsent(t *testing.T, s statestore.StoreI) {
	require.NoError(t, s.DeleteColumnWidth("play", statestore.ColWidthTierColumn, "", "never-written"))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func colWidthUnknownApp(t *testing.T, s statestore.StoreI) {
	rows, err := s.ListColumnWidths("nobody")
	require.NoError(t, err)
	assert.NotNil(t, rows, "an empty answer is an empty slice, not nil")
	assert.Empty(t, rows)
}

// An app's overrides must not include those of an app nested under its id.
func colWidthNestedAppIds(t *testing.T, s statestore.StoreI) {
	t0 := past()
	require.NoError(t, s.WriteColumnWidth(cw("x", statestore.ColWidthTierColumn, "", "k", 10, t0)))
	require.NoError(t, s.WriteColumnWidth(cw("x/child", statestore.ColWidthTierColumn, "", "k", 20, t0)))
	rows, err := s.ListColumnWidths("x")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 10.0, rows[0].Points)
}

func colWidthRoundTrip(t *testing.T, s statestore.StoreI) {
	t0 := past()
	want := statestore.ColumnWidthRow{
		AppId: "play", InstanceKey: 42, Tier: statestore.ColWidthTierInstance,
		Scope: "master/table", ColumnKey: "0123abcd", Points: 137.5, FontSize: 13.25, Ts: t0,
	}
	require.NoError(t, s.WriteColumnWidth(want))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	got := rows[0]
	assert.WithinDuration(t, want.Ts, got.Ts, time.Millisecond)
	got.Ts = want.Ts
	assert.Equal(t, want, got)
}
