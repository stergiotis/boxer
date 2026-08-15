package factsstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryFactsStore_WriteGrant_AssignsId(t *testing.T) {
	s := NewInMemoryFactsStore()
	id1, err := s.WriteGrant(GrantRow{AppId: "a", Pattern: "x"})
	require.NoError(t, err)
	id2, err := s.WriteGrant(GrantRow{AppId: "b", Pattern: "y"})
	require.NoError(t, err)
	assert.NotZero(t, id1)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, s.Grants(), 2)
}

func TestInMemoryFactsStore_WriteAudit(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteAudit(AuditRow{AppId: "a", Subject: "ch.query.boxer", Result: "ok", LatencyMs: 5})
	require.NoError(t, err)
	rows := s.AuditRows()
	require.Len(t, rows, 1)
	assert.Equal(t, "ch.query.boxer", rows[0].Subject)
}

// Workingset trail (ADR-0148 §SD6) — persist-state semantics: append-only,
// reverse-scan latest, tombstone delete, isolated per (app, name).

func TestInMemoryFactsStore_Workingset_LatestWins(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v1"),
	})
	require.NoError(t, err)
	_, err = s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v2"),
	})
	require.NoError(t, err)
	cfg, kind, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(cfg))
	assert.Equal(t, "playLaunch", kind, "kind is a stored column, not sniffed from the bytes")
	assert.Len(t, s.Workingsets(), 2, "the trail keeps both writes")
}

func TestInMemoryFactsStore_Workingset_Missing(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, _, found, err := s.LatestWorkingset("nope", "default")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryFactsStore_DeleteWorkingset_Tombstones(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v1"),
	})
	require.NoError(t, err)
	require.NoError(t, s.DeleteWorkingset("play", "default"))
	_, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.False(t, found)
	// A write after the tombstone reads back again — the tombstone marks a
	// point in the trail, it does not close the name.
	_, err = s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v3"),
	})
	require.NoError(t, err)
	cfg, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v3", string(cfg))
}

func TestInMemoryFactsStore_Workingset_NameIsolation(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("d"),
	})
	require.NoError(t, err)
	_, err = s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "scratch", Kind: "playLaunch", Config: []byte("s"),
	})
	require.NoError(t, err)
	cfg, _, _, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.Equal(t, "d", string(cfg))
	cfg, _, _, err = s.LatestWorkingset("play", "scratch")
	require.NoError(t, err)
	assert.Equal(t, "s", string(cfg))
	// …and the same name under another app is a different record.
	_, _, found, err := s.LatestWorkingset("imztop", "default")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryFactsStore_WriteWorkingset_DefensiveCopy(t *testing.T) {
	s := NewInMemoryFactsStore()
	cfg := []byte("hello")
	_, err := s.WriteWorkingset(WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: cfg,
	})
	require.NoError(t, err)
	cfg[0] = 'X'
	got, _, _, _ := s.LatestWorkingset("play", "default")
	assert.Equal(t, "hello", string(got))
}

// ListWorkingsets (ADR-0148 §SD7) — the stored set, not the trail.

func TestInMemoryFactsStore_ListWorkingsets_LatestPerKey(t *testing.T) {
	s := NewInMemoryFactsStore()
	t0 := time.Now().UTC()
	for _, row := range []WorkingsetRow{
		{RunId: "r1", AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v1"), TileKey: 1, Reason: "user-close", Ts: t0},
		{RunId: "r2", AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v2"), TileKey: 2, Reason: "shutdown", Ts: t0.Add(time.Second)},
		{RunId: "r2", AppId: "play", Name: "scratch", Kind: "playLaunch", Config: []byte("s"), TileKey: 3, Ts: t0},
		{RunId: "r2", AppId: "imztop", Name: "default", Kind: "imztopLaunch", Config: []byte("i"), TileKey: 4, Ts: t0},
	} {
		_, err := s.WriteWorkingset(row)
		require.NoError(t, err)
	}
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 3, "one row per (app, name), not per write")
	// Sorted by AppId then Name.
	assert.Equal(t, []string{"imztop/default", "play/default", "play/scratch"},
		[]string{
			string(rows[0].AppId) + "/" + rows[0].Name,
			string(rows[1].AppId) + "/" + rows[1].Name,
			string(rows[2].AppId) + "/" + rows[2].Name,
		})
	// The winner is the newest write, with its own provenance.
	assert.Equal(t, "v2", string(rows[1].Config))
	assert.Equal(t, "shutdown", rows[1].Reason)
	assert.EqualValues(t, 2, rows[1].TileKey)
	assert.Equal(t, "r2", rows[1].RunId)
	assert.Equal(t, "playLaunch", rows[1].Kind)
	assert.Equal(t, t0.Add(time.Second), rows[1].Ts, "Ts is the winning row's write time")
}

func TestInMemoryFactsStore_ListWorkingsets_Empty(t *testing.T) {
	rows, err := NewInMemoryFactsStore().ListWorkingsets()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestInMemoryFactsStore_ListWorkingsets_TombstoneExcludesKey(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteWorkingset(WorkingsetRow{AppId: "play", Name: "default", Config: []byte("v1")})
	require.NoError(t, err)
	_, err = s.WriteWorkingset(WorkingsetRow{AppId: "imztop", Name: "default", Config: []byte("keep")})
	require.NoError(t, err)
	require.NoError(t, s.DeleteWorkingset("play", "default"))

	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 1, "a tombstoned key is absent, and its earlier write must not stand in for it")
	assert.EqualValues(t, "imztop", rows[0].AppId)

	// A write after the tombstone brings the key back.
	_, err = s.WriteWorkingset(WorkingsetRow{AppId: "play", Name: "default", Config: []byte("v2")})
	require.NoError(t, err)
	rows, err = s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "v2", string(rows[1].Config))
}

func TestInMemoryFactsStore_ListWorkingsets_DefensiveCopy(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteWorkingset(WorkingsetRow{AppId: "play", Name: "default", Config: []byte("hello")})
	require.NoError(t, err)
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	rows[0].Config[0] = 'X'
	rows, err = s.ListWorkingsets()
	require.NoError(t, err)
	assert.Equal(t, "hello", string(rows[0].Config), "the caller must not be able to edit the store")
}

// Column-width overrides (ADR-0151). The behaviours asserted here are the
// contract chstore's implementation must match; the CH-side tests mirror
// them so the two backends cannot drift silently.

func TestInMemoryFactsStore_ColumnWidth_LatestPerKey(t *testing.T) {
	s := NewInMemoryFactsStore()
	t0 := time.Now().UTC()
	for _, row := range []ColumnWidthRow{
		{AppId: "play", Tier: ColWidthTierInstance, Scope: "attrs", ColumnKey: "k1", Points: 100, FontSize: 12, Ts: t0},
		{AppId: "play", Tier: ColWidthTierInstance, Scope: "attrs", ColumnKey: "k1", Points: 140, FontSize: 12, Ts: t0.Add(time.Second)},
		{AppId: "play", Tier: ColWidthTierColumn, ColumnKey: "k1", Points: 90, FontSize: 12, Ts: t0},
		{AppId: "imztop", Tier: ColWidthTierColumn, ColumnKey: "k1", Points: 50, FontSize: 12, Ts: t0},
	} {
		_, err := s.WriteColumnWidth(row)
		require.NoError(t, err)
	}

	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 2, "one row per key, not per write")
	// SortColumnWidths orders by tier: "column" precedes "instance".
	assert.Equal(t, ColWidthTierColumn, rows[0].Tier)
	assert.Equal(t, 90.0, rows[0].Points)
	assert.Equal(t, ColWidthTierInstance, rows[1].Tier)
	assert.Equal(t, 140.0, rows[1].Points, "the later write wins")
}

// The tier is part of the identity: an instance-tier and a column-tier
// entry for the same column key are different overrides, and one must not
// collapse onto the other.
func TestInMemoryFactsStore_ColumnWidth_TierIsPartOfIdentity(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteColumnWidth(ColumnWidthRow{AppId: "play", Tier: ColWidthTierInstance, Scope: "t", ColumnKey: "k", Points: 10})
	require.NoError(t, err)
	_, err = s.WriteColumnWidth(ColumnWidthRow{AppId: "play", Tier: ColWidthTierColumn, ColumnKey: "k", Points: 20})
	require.NoError(t, err)

	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

// A cleared override stays cleared. Writing, clearing, then writing again
// must resurrect it — and clearing must not let an older surviving write
// show through, which is the failure mode a WHERE-based tombstone filter
// produces on the CH side.
func TestInMemoryFactsStore_ColumnWidth_DeleteThenWrite(t *testing.T) {
	s := NewInMemoryFactsStore()
	k := ColumnWidthRow{AppId: "play", Tier: ColWidthTierColumn, ColumnKey: "k"}

	k.Points = 10
	_, err := s.WriteColumnWidth(k)
	require.NoError(t, err)
	k.Points = 20
	_, err = s.WriteColumnWidth(k)
	require.NoError(t, err)

	require.NoError(t, s.DeleteColumnWidth("play", ColWidthTierColumn, "", "k"))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows, "a cleared override must not fall back to an older write")

	k.Points = 30
	_, err = s.WriteColumnWidth(k)
	require.NoError(t, err)
	rows, err = s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 30.0, rows[0].Points)
}

func TestInMemoryFactsStore_ColumnWidth_DeleteAbsentKey(t *testing.T) {
	s := NewInMemoryFactsStore()
	require.NoError(t, s.DeleteColumnWidth("play", ColWidthTierColumn, "", "never-written"))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestInMemoryFactsStore_ColumnWidth_EmptyForUnknownApp(t *testing.T) {
	s := NewInMemoryFactsStore()
	rows, err := s.ListColumnWidths("nobody")
	require.NoError(t, err)
	assert.Empty(t, rows)
}
