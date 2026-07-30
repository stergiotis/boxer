package factsstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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

func TestInMemoryFactsStore_State_LatestWins(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: []byte("v1"), Ts: time.Now()})
	require.NoError(t, err)
	_, err = s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: []byte("v2"), Ts: time.Now()})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("v2"), got)
}

func TestInMemoryFactsStore_State_MissingKey(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, found, err := s.LatestState("nope", "absent")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryFactsStore_DeleteState_Tombstones(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: []byte("v1"), Ts: time.Now()})
	require.NoError(t, err)
	err = s.DeleteState("play", "tabs")
	require.NoError(t, err)
	_, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryFactsStore_WriteState_DefensiveCopy(t *testing.T) {
	s := NewInMemoryFactsStore()
	v := []byte("hello")
	_, err := s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: v, Ts: time.Now()})
	require.NoError(t, err)
	v[0] = 'X'
	got, _, _ := s.LatestState("play", "tabs")
	assert.Equal(t, "hello", string(got))
}

func TestInMemoryFactsStore_StateSeparation_TwoApps(t *testing.T) {
	s := NewInMemoryFactsStore()
	require.NoError(t, mustWrite(s, "play", "tabs", []byte("p")))
	require.NoError(t, mustWrite(s, "imztop", "tabs", []byte("i")))
	got, _, _ := s.LatestState("play", "tabs")
	assert.Equal(t, "p", string(got))
	got, _, _ = s.LatestState("imztop", "tabs")
	assert.Equal(t, "i", string(got))
}

func mustWrite(s FactsStoreI, appId app.AppIdT, key string, value []byte) (err error) {
	_, err = s.WriteState(StateRow{AppId: appId, Key: key, Value: value, Ts: time.Now()})
	return
}

// Workingset trail (ADR-0148 §SD6) — LatestState semantics: append-only,
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
