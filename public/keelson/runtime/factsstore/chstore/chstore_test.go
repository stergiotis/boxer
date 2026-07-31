package chstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
)

// newLiveStore constructs a chstore against the project's localhost CH on
// a per-test database, runs DDL, and returns the store with a cleanup. The
// caller defers cleanup. Skips the calling test when CH is unreachable.
func newLiveStore(t *testing.T) (s *chstore.Store, cleanup func()) {
	t.Helper()
	cfg := chstore.Defaults()
	cfg.Database = "runtime_chstore_test"
	ctx := context.Background()
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	if err := s.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	// Reset any leftover state from a prior run. Drop the table
	// (not just truncate) so a schema migration in a prior commit
	// doesn't leave a stale-columns table in place.
	require.NoError(t, s.DropTable(ctx))
	require.NoError(t, s.SetupTable(ctx, "MergeTree() ORDER BY tuple()"))
	cleanup = func() {
		_ = s.DropTable(context.Background())
	}
	return
}

func TestStore_New_NoLiveCH(t *testing.T) {
	// New itself should succeed without contacting CH.
	cfg := chstore.Defaults()
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestStore_New_RejectsEmptyConfig(t *testing.T) {
	_, err := chstore.New(chstore.Config{})
	require.Error(t, err)
}

func TestStore_WriteGrant_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	id, err := s.WriteGrant(factsstore.GrantRow{
		AppId:      "github.com/example/play",
		Pattern:    "ch.query.boxer",
		Direction:  app.CapDirectionPub,
		Reason:     "test grant",
		Sticky:     true,
		GrantedVia: "auto",
		Ts:         time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.NotZero(t, id)
	n, err := s.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), n)
}

func TestStore_WriteAudit_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	id, err := s.WriteAudit(factsstore.AuditRow{
		AppId:         "github.com/example/play",
		Subject:       "ch.query.boxer",
		Result:        "ok",
		LatencyMs:     7,
		RequestSizeB:  120,
		ResponseSizeB: 4096,
		Ts:            time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.NotZero(t, id)
	n, err := s.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), n)
}

func TestStore_WriteState_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	id, err := s.WriteState(factsstore.StateRow{
		AppId: "github.com/example/play",
		Key:   "tabs",
		Value: []byte(`[{"name":"main"}]`),
		Ts:    time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.NotZero(t, id)
	n, err := s.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), n)
}

func TestStore_AllThreeKinds_OneTable_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteGrant(factsstore.GrantRow{
		AppId: "a", Pattern: "x.y", Direction: app.CapDirectionPub, Reason: "r", Sticky: false, GrantedVia: "auto", Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = s.WriteAudit(factsstore.AuditRow{
		AppId: "a", Subject: "x.y", Result: "ok", LatencyMs: 1, Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = s.WriteState(factsstore.StateRow{
		AppId: "a", Key: "k", Value: []byte("v"), Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	n, err := s.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(3), n)
}

func TestStore_WriteLog_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	id, err := s.WriteLog(factsstore.LogRow{
		AppId:   "github.com/example/play",
		Level:   "info",
		Message: "query ok",
		Caller:  "play.go:42",
		Service: "play",
		Fields: []factsstore.LogField{
			{Name: "subject", Kind: factsstore.LogFieldKindString, Str: "ch.query.boxer"},
			{Name: "latency_ms", Kind: factsstore.LogFieldKindInt, Int: 7},
			{Name: "ok", Kind: factsstore.LogFieldKindBool, Bool: true},
			{Name: "rate", Kind: factsstore.LogFieldKindFloat, Float: 3.14},
		},
		Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.NotZero(t, id)
	n, err := s.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(1), n)
}

func TestStore_WriteLog_WithError_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	id, err := s.WriteLog(factsstore.LogRow{
		AppId:   "github.com/example/play",
		Level:   "error",
		Message: "ch query failed",
		Error:   "connection refused",
		Stack:   "goroutine 1 [running]:\nmain.go:10",
		Ts:      time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.NotZero(t, id)
}

func TestStore_RecentLogs_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteLog(factsstore.LogRow{
		AppId: "play", Level: "info", Message: "first", Caller: "play.go:1", Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{
		AppId: "play", Level: "warn", Message: "second", Caller: "play.go:2", Ts: t0.Add(time.Millisecond),
	})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{
		AppId: "imztop", Level: "info", Message: "third", Ts: t0.Add(2 * time.Millisecond),
	})
	require.NoError(t, err)

	rows, err := s.RecentLogs(context.Background(), chstore.LogFilter{})
	require.NoError(t, err)
	assert.Len(t, rows, 3, "all log rows should round-trip with no filter")

	// Newest first.
	if assert.GreaterOrEqual(t, len(rows), 3) {
		assert.Equal(t, "third", rows[0].Message)
		assert.Equal(t, "second", rows[1].Message)
		assert.Equal(t, "first", rows[2].Message)
	}
}

func TestStore_RecentLogs_FilterByApp_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteLog(factsstore.LogRow{AppId: "play", Level: "info", Message: "P1", Ts: t0})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{AppId: "imztop", Level: "info", Message: "I1", Ts: t0.Add(time.Millisecond)})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{AppId: "play", Level: "warn", Message: "P2", Ts: t0.Add(2 * time.Millisecond)})
	require.NoError(t, err)

	rows, err := s.RecentLogs(context.Background(), chstore.LogFilter{AppId: "play"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "P2", rows[0].Message)
	assert.Equal(t, "P1", rows[1].Message)
	for _, r := range rows {
		assert.Equal(t, "play", string(r.AppId))
	}
}

func TestStore_RecentLogs_FilterByLevel_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteLog(factsstore.LogRow{AppId: "play", Level: "info", Message: "i1", Ts: t0})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{AppId: "play", Level: "warn", Message: "w1", Ts: t0.Add(time.Millisecond)})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{AppId: "play", Level: "error", Message: "e1", Ts: t0.Add(2 * time.Millisecond)})
	require.NoError(t, err)

	rows, err := s.RecentLogs(context.Background(), chstore.LogFilter{Level: "error"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "e1", rows[0].Message)
}

func TestStore_RecentLogs_FilterByTimeRange_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	// boxer.facts `ts` column is DateTime('UTC') (second precision),
	// so the test spaces rows two seconds apart to make the window
	// boundary unambiguous. logbridge enqueues sub-second events under
	// load; ordering within a second falls back to the `id` column.
	t0 := time.Now().Add(-10 * time.Second).Truncate(time.Second).UTC()
	_, err := s.WriteLog(factsstore.LogRow{AppId: "play", Level: "info", Message: "old", Ts: t0})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{AppId: "play", Level: "info", Message: "mid", Ts: t0.Add(2 * time.Second)})
	require.NoError(t, err)
	_, err = s.WriteLog(factsstore.LogRow{AppId: "play", Level: "info", Message: "new", Ts: t0.Add(4 * time.Second)})
	require.NoError(t, err)

	rows, err := s.RecentLogs(context.Background(), chstore.LogFilter{
		Since: t0.Add(1 * time.Second),
		Until: t0.Add(3 * time.Second),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "only mid should fall inside the [t0+1s, t0+3s) window")
	assert.Equal(t, "mid", rows[0].Message)
}

func TestStore_RecentLogs_RecoversStackAndErr_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteLog(factsstore.LogRow{
		AppId:   "play",
		Level:   "error",
		Message: "ch query failed",
		Error:   "connection refused",
		Stack:   "goroutine 1 [running]:\nmain.go:10",
		Caller:  "ch.go:42",
		Service: "play-svc",
		Ts:      time.Now().UTC(),
	})
	require.NoError(t, err)
	rows, err := s.RecentLogs(context.Background(), chstore.LogFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	got := rows[0]
	assert.Equal(t, "error", got.Level)
	assert.Equal(t, "ch query failed", got.Message)
	assert.Equal(t, "connection refused", got.Error)
	assert.Equal(t, "goroutine 1 [running]:\nmain.go:10", got.Stack,
		"text section round-trip must preserve embedded newlines")
	assert.Equal(t, "ch.go:42", got.Caller)
	assert.Equal(t, "play-svc", got.Service)
}

func TestStore_RecentLogs_Empty_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	rows, err := s.RecentLogs(context.Background(), chstore.LogFilter{})
	require.NoError(t, err)
	assert.NotNil(t, rows, "RecentLogs must return non-nil even when empty")
	assert.Empty(t, rows)
}

func TestStore_LatestState_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	want := []byte(`[{"name":"main"}]`)
	_, err := s.WriteState(factsstore.StateRow{
		AppId: "play", Key: "tabs", Value: want, Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, got)
}

func TestStore_LatestState_MissingKey_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, found, err := s.LatestState("play", "absent-key")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestStore_LatestState_LatestWins_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteState(factsstore.StateRow{
		AppId: "play", Key: "tabs", Value: []byte("v1"), Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteState(factsstore.StateRow{
		AppId: "play", Key: "tabs", Value: []byte("v2"), Ts: t0.Add(time.Second),
	})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(got))
}

func TestStore_LatestState_AppIsolation_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteState(factsstore.StateRow{AppId: "play", Key: "tabs", Value: []byte("p"), Ts: time.Now().UTC()})
	require.NoError(t, err)
	_, err = s.WriteState(factsstore.StateRow{AppId: "imztop", Key: "tabs", Value: []byte("i"), Ts: time.Now().UTC()})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "p", string(got))
	got, found, err = s.LatestState("imztop", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "i", string(got))
}

func TestStore_DeleteState_Tombstones_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteState(factsstore.StateRow{
		AppId: "play", Key: "tabs", Value: []byte("v1"), Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	// Sanity: present.
	_, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	require.True(t, found)
	// Tombstone.
	err = s.DeleteState("play", "tabs")
	require.NoError(t, err)
	_, found, err = s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.False(t, found, "tombstone should hide the prior value")
}

func TestStore_DeleteState_ThenWrite_Resurrects_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteState(factsstore.StateRow{AppId: "play", Key: "k", Value: []byte("v1"), Ts: t0})
	require.NoError(t, err)
	err = s.DeleteState("play", "k")
	require.NoError(t, err)
	// Write again later in time — should reappear.
	_, err = s.WriteState(factsstore.StateRow{AppId: "play", Key: "k", Value: []byte("v2"), Ts: t0.Add(2 * time.Second)})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "k")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(got))
}

func TestStore_LatestState_BinaryValue_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	binary := []byte{0x00, 0xFF, 0x10, 0x7F, 0x80, 0xCA, 0xFE}
	_, err := s.WriteState(factsstore.StateRow{
		AppId: "play", Key: "blob", Value: binary, Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "blob")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, binary, got, "hex transport must preserve raw bytes")
}

// Workingset trail (ADR-0148 §SD6). Same probe-and-skip shape as the
// LatestState tests above — these need a live CH, and the package's
// convention is to skip rather than split the file across lanes.

func TestStore_LatestWorkingset_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	want := []byte{0x00, 0xFF, 0x10, 0x7F, 0x80, 0xCA, 0xFE}
	_, err := s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: want, TileKey: 7, Reason: "user-close", Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	cfg, kind, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, cfg, "hex transport must preserve raw bytes")
	assert.Equal(t, "playLaunch", kind, "kind reads back as a column, never sniffed")
}

func TestStore_LatestWorkingset_Missing_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestStore_LatestWorkingset_LatestWins_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("v1"), TileKey: 1, Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("v2"), TileKey: 2, Ts: t0.Add(time.Second),
	})
	require.NoError(t, err)
	cfg, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(cfg))
}

func TestStore_LatestWorkingset_NameAndAppIsolation_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	ts := time.Now().UTC()
	_, err := s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("d"), TileKey: 1, Ts: ts,
	})
	require.NoError(t, err)
	_, err = s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "scratch", Kind: "playLaunch",
		Config: []byte("s"), TileKey: 2, Ts: ts,
	})
	require.NoError(t, err)
	cfg, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "d", string(cfg))
	cfg, _, found, err = s.LatestWorkingset("play", "scratch")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "s", string(cfg))
	_, _, found, err = s.LatestWorkingset("imztop", "default")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestStore_DeleteWorkingset_Tombstones_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC()
	_, err := s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("v1"), TileKey: 1, Ts: t0,
	})
	require.NoError(t, err)
	require.NoError(t, s.DeleteWorkingset("play", "default"))
	_, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.False(t, found, "tombstone should hide the prior record")
	// A later write resurrects the name.
	_, err = s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "run-1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("v2"), TileKey: 2, Ts: t0.Add(2 * time.Second),
	})
	require.NoError(t, err)
	cfg, _, found, err := s.LatestWorkingset("play", "default")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "v2", string(cfg))
}

// ListWorkingsets (ADR-0148 §SD7) — the same probe-and-skip convention;
// these mirror the in-memory tests so the two backends are held to one
// contract.

func TestStore_ListWorkingsets_Empty_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestStore_ListWorkingsets_LatestPerKey_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second)
	binary := []byte{0x00, 0xFF, 0x10, 0x7F, 0x80, 0xCA, 0xFE}
	for _, row := range []factsstore.WorkingsetRow{
		{RunId: "r1", AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("v1"), TileKey: 1, Reason: "user-close", Ts: t0},
		{RunId: "r2", AppId: "play", Name: "default", Kind: "playLaunch", Config: binary, TileKey: 2, Reason: "shutdown", Ts: t0.Add(time.Second)},
		{RunId: "r2", AppId: "play", Name: "scratch", Kind: "playLaunch", Config: []byte("s"), TileKey: 3, Ts: t0},
		{RunId: "r2", AppId: "imztop", Name: "default", Kind: "imztopLaunch", Config: []byte("i"), TileKey: 4, Ts: t0},
	} {
		_, err := s.WriteWorkingset(row)
		require.NoError(t, err)
	}
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 3, "one row per (app, name), not per write")
	assert.Equal(t, []string{"imztop/default", "play/default", "play/scratch"},
		[]string{
			string(rows[0].AppId) + "/" + rows[0].Name,
			string(rows[1].AppId) + "/" + rows[1].Name,
			string(rows[2].AppId) + "/" + rows[2].Name,
		})
	won := rows[1]
	assert.Equal(t, binary, won.Config, "hex transport must preserve raw bytes")
	assert.Equal(t, "playLaunch", won.Kind)
	assert.Equal(t, "shutdown", won.Reason)
	assert.EqualValues(t, 2, won.TileKey)
	assert.Equal(t, "r2", won.RunId)
	assert.Equal(t, t0.Add(time.Second), won.Ts, "Ts is the winning row's write time")
}

func TestStore_ListWorkingsets_TombstoneExcludesKey_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second)
	_, err := s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "r1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("v1"), TileKey: 1, Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "r1", AppId: "imztop", Name: "default", Kind: "imztopLaunch",
		Config: []byte("keep"), TileKey: 2, Ts: t0,
	})
	require.NoError(t, err)
	require.NoError(t, s.DeleteWorkingset("play", "default"))

	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 1, "the tombstone must be judged on the winning row, not filtered out of the candidates")
	assert.EqualValues(t, "imztop", rows[0].AppId)

	// A write after the tombstone brings the key back.
	_, err = s.WriteWorkingset(factsstore.WorkingsetRow{
		RunId: "r1", AppId: "play", Name: "default", Kind: "playLaunch",
		Config: []byte("v2"), TileKey: 3, Ts: time.Now().UTC().Add(time.Minute),
	})
	require.NoError(t, err)
	rows, err = s.ListWorkingsets()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "v2", string(rows[1].Config))
}

func TestStore_ListWorkingsets_IgnoresOtherKinds_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	// A launch row shares the workingset row's whole vocabulary except the
	// kind tag (ADR-0148 §SD6), so the kind predicate is what separates them.
	_, err := s.WriteLaunch(factsstore.LaunchRow{
		RunId: "r1", CallerAppId: "imztop", TargetAppId: "play",
		TileKey: 9, ConfigKind: "playLaunch", Config: []byte("not-a-workingset"),
		Ts: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = s.WriteState(factsstore.StateRow{AppId: "play", Key: "k", Value: []byte("v")})
	require.NoError(t, err)
	rows, err := s.ListWorkingsets()
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// Column-width overrides (ADR-0151). These mirror the InMemoryFactsStore
// tests of the same names so the two backends cannot drift: the collapse
// happens server-side here and in a reverse scan there, and only running
// both shapes against the same assertions keeps them honest.

func TestStore_ListColumnWidths_Empty_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestStore_ListColumnWidths_LatestPerKey_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second)
	for _, row := range []factsstore.ColumnWidthRow{
		{AppId: "play", Tier: factsstore.ColWidthTierInstance, Scope: "attrs", ColumnKey: "k1", Points: 100, FontSize: 12, Ts: t0},
		{AppId: "play", Tier: factsstore.ColWidthTierInstance, Scope: "attrs", ColumnKey: "k1", Points: 140.5, FontSize: 13.5, Ts: t0.Add(time.Second)},
		{AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: "k1", Points: 90, FontSize: 12, Ts: t0},
		{AppId: "imztop", Tier: factsstore.ColWidthTierColumn, ColumnKey: "k1", Points: 50, FontSize: 12, Ts: t0},
	} {
		_, err := s.WriteColumnWidth(row)
		require.NoError(t, err)
	}

	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 2, "one row per key, not per write; and imztop's row must not leak in")
	assert.Equal(t, factsstore.ColWidthTierColumn, rows[0].Tier)
	assert.Empty(t, rows[0].Scope, "the column tier carries no scope")
	assert.InDelta(t, 90.0, rows[0].Points, 1e-9)
	assert.Equal(t, factsstore.ColWidthTierInstance, rows[1].Tier)
	assert.Equal(t, "attrs", rows[1].Scope)
	assert.InDelta(t, 140.5, rows[1].Points, 1e-9, "the later write wins")
	assert.InDelta(t, 13.5, rows[1].FontSize, 1e-9, "font size travels with the width")
	assert.Equal(t, app.AppIdT("play"), rows[1].AppId)
}

func TestStore_ColumnWidth_TierIsPartOfIdentity_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "play", Tier: factsstore.ColWidthTierInstance, Scope: "t", ColumnKey: "k", Points: 10})
	require.NoError(t, err)
	_, err = s.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: "k", Points: 20})
	require.NoError(t, err)

	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

// The HAVING-vs-WHERE trap, asserted directly: a WHERE-based tombstone
// filter would return the surviving Points=10 row after the delete.
func TestStore_ColumnWidth_DeleteThenWrite_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	// Timestamps must sit in the past: DeleteColumnWidth stamps itself at
	// now, and a write dated into the future would out-rank the tombstone
	// on the (ts, id) sort key and legitimately win. That is the contract,
	// not a bug — but it is not what a real capture does.
	t0 := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	row := factsstore.ColumnWidthRow{AppId: "play", Tier: factsstore.ColWidthTierColumn, ColumnKey: "k", FontSize: 12}

	row.Points, row.Ts = 10, t0
	_, err := s.WriteColumnWidth(row)
	require.NoError(t, err)
	row.Points, row.Ts = 20, t0.Add(time.Second)
	_, err = s.WriteColumnWidth(row)
	require.NoError(t, err)

	require.NoError(t, s.DeleteColumnWidth("play", factsstore.ColWidthTierColumn, "", "k"))
	rows, err := s.ListColumnWidths("play")
	require.NoError(t, err)
	assert.Empty(t, rows, "a cleared override must not fall back to an older write")

	// Zero Ts takes defaultTs (now), so the resurrect lands at or after the
	// tombstone's second and wins the tie on the monotonic entity id.
	row.Points, row.Ts = 30, time.Time{}
	_, err = s.WriteColumnWidth(row)
	require.NoError(t, err)
	rows, err = s.ListColumnWidths("play")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.InDelta(t, 30.0, rows[0].Points, 1e-9)
}
