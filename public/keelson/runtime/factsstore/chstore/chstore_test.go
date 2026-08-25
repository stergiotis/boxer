package chstore_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// newLiveStore constructs a chstore against the project's localhost CH on
// a per-test database, runs DDL, and returns the store with a cleanup. The
// caller defers cleanup. Skips the calling test when CH is unreachable.
func newLiveStore(t *testing.T) (s *chstore.Store, cleanup func()) {
	t.Helper()
	// ConfigFromEnv, not Defaults: a CH that wants CLICKHOUSE_USER /
	// CLICKHOUSE_PASSWORD would otherwise answer /ping and then reject the
	// DDL, failing the test instead of skipping it.
	cfg := chstore.ConfigFromEnv()
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
	_, err = s.WriteLaunch(factsstore.LaunchRow{
		RunId: "r1", CallerAppId: "a", TargetAppId: "b", TileKey: 1, Ts: time.Now().UTC(),
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

// Workingset trail (ADR-0148 §SD6). Same probe-and-skip shape as the
// tests above — these need a live CH, and the package's convention is to
// skip rather than split the file across lanes.

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
	_, err = s.WriteAudit(factsstore.AuditRow{AppId: "play", Subject: "x.y", Result: "ok", Ts: time.Now().UTC()})
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

// --- ADR-0191 instance attribution ------------------------------------------

// newLiveStampedStore is newLiveStore with a configured run id, so the
// §SD3 store-level stamp is active.
func newLiveStampedStore(t *testing.T, runId string) (s *chstore.Store, cleanup func()) {
	t.Helper()
	cfg := chstore.ConfigFromEnv()
	cfg.Database = "runtime_chstore_test"
	cfg.RunId = runId
	ctx := context.Background()
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	if err := s.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	require.NoError(t, s.DropTable(ctx))
	require.NoError(t, s.SetupTable(ctx, "MergeTree() ORDER BY tuple()"))
	cleanup = func() { _ = s.DropTable(context.Background()) }
	return
}

// readClient is a plain HTTP client onto the same server the store writes to.
// The store exposes no generic query verb — by design, its readers are the
// per-kind ones — so a test asserting the physical encoding brings its own.
func readClient() *chclient.Client {
	return chclient.New(chclient.ConfigFromEnv(), nil)
}

// attributionOf reads back the run id and instance key of the single row in
// the test table, through the physical lanes rather than through LW_GET: this
// package ships raw SQL and runs no client-side expansion pass, so naming a
// membership here is not available. The membership IDS are the point of the
// assertion anyway — a test that resolved them the same way the writer does
// would agree with itself.
//
// The two lanes are read by different idioms because they are shaped
// differently, and mixing them up is the mistake this comment exists to stop
// the next reader repeating. On the MIXED channel the parameter lane (mrhp)
// is co-indexed with the membership lane (lmr), so arrayFirst over that PAIR
// is sound — the value lane is not co-indexed with either and pairing it with
// lmr fails outright ("arrays passed to arrayFirst must have equal size"). On
// the LOW-CARD-REF channel the value lane is a ragged run per attribute, so
// the position comes from the cumulative sum of the cardinality lane. Both
// forms are the ones runsessions.go already composes for its readers.
func attributionOf(t *testing.T) (runId string, instanceKey string) {
	t.Helper()
	const (
		symLMR    = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
		symMRHP   = "`tv:symbol:mrhp:mrhp:y:4:::0::data`"
		u64Value  = "`tv:u64Array:value:val:u64h:4:::0::data`"
		u64LR     = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
		u64LRCard = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
	)
	tileKey := vocab.MembLifecycleTileKey.GetId().Value()
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", u64LR, tileKey)
	sql := fmt.Sprintf(`
SELECT
  arrayFirst((p, m) -> m = %d, %s, %s) AS run_id,
  toString(if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), 0)) AS instance_key
FROM runtime_chstore_test.facts
LIMIT 1
FORMAT TabSeparated`,
		vocab.MembRuntimeRun.GetId().Value(), symMRHP, symLMR,
		idxInLr, u64Value, u64LRCard, idxInLr)

	body, err := readClient().Query(context.Background(), sql)
	require.NoError(t, err)
	defer body.Close()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	parts := strings.Split(strings.TrimRight(string(raw), "\n"), "\t")
	require.Len(t, parts, 2, "expected run_id and instance_key, got %q", string(raw))
	return parts[0], parts[1]
}

// TestStore_StampsRunAndInstance_LiveCH is the ADR-0191 round-trip: every
// kind carries the run (§SD3, from the store) and the window (§SD4, from the
// row), on the memberships §SD1 reuses.
//
// One case per kind that gained something, because the stamp is written per
// writer and a kind that forgets it is silent — the row lands, reads back,
// and is simply unattributable. The three kinds that already carried a run
// are covered by their own tests above.
func TestStore_StampsRunAndInstance_LiveCH(t *testing.T) {
	const runId = "run-0191"
	ts := time.Now().UTC()

	for _, tc := range []struct {
		kind string
		want string // expected instance key, as read back
		//nolint:revive // the writer under test, one per kind
		write func(s *chstore.Store) error
	}{
		{kind: "audit", want: "42", write: func(s *chstore.Store) error {
			_, err := s.WriteAudit(factsstore.AuditRow{
				AppId: "github.com/example/play", InstanceKey: 42,
				Subject: "ch.query.boxer", Result: "ok", Ts: ts,
			})
			return err
		}},
		{kind: "grant", want: "7", write: func(s *chstore.Store) error {
			_, err := s.WriteGrant(factsstore.GrantRow{
				AppId: "github.com/example/play", InstanceKey: 7,
				Pattern: "ch.query.boxer", Direction: app.CapDirectionPub, Ts: ts,
			})
			return err
		}},
		{kind: "log", want: "9", write: func(s *chstore.Store) error {
			_, err := s.WriteLog(factsstore.LogRow{
				AppId: "github.com/example/play", InstanceKey: 9,
				Level: "info", Message: "hello", Ts: ts,
			})
			return err
		}},
		{kind: "columnWidth", want: "3", write: func(s *chstore.Store) error {
			_, err := s.WriteColumnWidth(factsstore.ColumnWidthRow{
				AppId: "github.com/example/play", InstanceKey: 3,
				Tier: factsstore.ColWidthTierColumn, ColumnKey: "k",
				Points: 100, FontSize: 12, Ts: ts,
			})
			return err
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s, cleanup := newLiveStampedStore(t, runId)
			defer cleanup()
			require.NoError(t, tc.write(s))

			gotRun, gotInstance := attributionOf(t)
			assert.Equal(t, runId, gotRun,
				"%s: the store must stamp its run — without it, limiting to a run is a timestamp range", tc.kind)
			assert.Equal(t, tc.want, gotInstance,
				"%s: the window must ride the row, or two windows of one app are indistinguishable", tc.kind)
		})
	}
}

// TestStore_RowRunIdWinsOverTheStores_LiveCH pins the precedence in §SD3: a
// DTO that carries its own run keeps it. They are equal on a live write, so
// the case that distinguishes them is a backfill — rows written on behalf of
// a run other than the writing process's.
func TestStore_RowRunIdWinsOverTheStores_LiveCH(t *testing.T) {
	s, cleanup := newLiveStampedStore(t, "run-of-this-process")
	defer cleanup()
	_, err := s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: "run-being-backfilled", AppId: "github.com/example/play",
		TileKey: 5, Phase: factsstore.AppLifecyclePhaseStarted, Ts: time.Now().UTC(),
	})
	require.NoError(t, err)

	gotRun, gotInstance := attributionOf(t)
	assert.Equal(t, "run-being-backfilled", gotRun)
	assert.Equal(t, "5", gotInstance)
}

// TestStore_UnstampedStoreWritesNoRun_LiveCH pins what absence means: a store
// built without a run id writes rows carrying none, exactly as before
// ADR-0191, rather than a row belonging to run "".
func TestStore_UnstampedStoreWritesNoRun_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteAudit(factsstore.AuditRow{
		AppId: "github.com/example/play", Subject: "x.y", Result: "ok", Ts: time.Now().UTC(),
	})
	require.NoError(t, err)

	gotRun, gotInstance := attributionOf(t)
	assert.Empty(t, gotRun)
	assert.Equal(t, "0", gotInstance, "an absent instance reads as the type default, not as window 0")
}

// --- ADR-0191 §SD7: the trail as rows -----------------------------------------

// TestStore_ListRunEvents_LiveCH is the read half of the decision: the kinds
// this store writes come back as flattened rows a consumer can render without
// holding the membership vocabulary.
//
// One row per kind that carries different identity, because the flattening is
// per-kind arithmetic and a kind that decodes wrong is silent — it lands with
// an empty detail or a zero window rather than failing.
func TestStore_ListRunEvents_LiveCH(t *testing.T) {
	const runId = "run-events-0191"
	s, cleanup := newLiveStampedStore(t, runId)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second)

	_, err := s.WriteRuntimeStart(factsstore.RuntimeStartRow{
		RunId: runId, Hostname: "box-1", Pid: 4242, GoVersion: "go1.26.5", Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 2,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0.Add(time.Second),
	})
	require.NoError(t, err)
	_, err = s.WriteAudit(factsstore.AuditRow{
		AppId: "github.com/example/play", InstanceKey: 2,
		Subject: "ch.query.boxer", Result: "ok", Ts: t0.Add(2 * time.Second),
	})
	require.NoError(t, err)

	rows, err := s.ListRunEvents(factsstore.RunEventFilter{RunId: runId, Since: t0})
	require.NoError(t, err)
	require.Len(t, rows, 3, "one row per written kind, oldest first")

	assert.Equal(t, "run start", rows[0].Kind)
	assert.Empty(t, rows[0].AppId, "a process-level event names no app")
	assert.Contains(t, rows[0].Detail, "box-1", "the row's own values, joined")
	assert.Contains(t, rows[0].Detail, "go1.26.5")
	assert.Equal(t, factsstore.RunEventSourceFacts, rows[0].Source)

	assert.Equal(t, "lifecycle", rows[1].Kind)
	assert.Equal(t, "github.com/example/play", string(rows[1].AppId))
	assert.Equal(t, uint64(2), rows[1].InstanceKey)
	assert.Equal(t, runId, rows[1].RunId)
	assert.Equal(t, "started", rows[1].Detail)
	assert.NotContains(t, rows[1].Detail, "github.com/example/play",
		"the app is its own column and must not be repeated into the detail")

	assert.Equal(t, "audit", rows[2].Kind)
	assert.Equal(t, uint64(2), rows[2].InstanceKey,
		"an audit row now lanes with the lifecycle row above it")
	assert.Contains(t, rows[2].Detail, "ch.query.boxer")
	assert.Contains(t, rows[2].Detail, "ok")
}

// TestStore_ListRunEvents_SelectsOneRun_LiveCH pins the attribution rule: a
// row naming another run is out, and a row naming NO run is in only when it
// landed after this one started. That second clause is the compromise
// ADR-0191 leaves for rows written before it — and the reason two overlapping
// boxer processes blend in the historical part of a trail.
func TestStore_ListRunEvents_SelectsOneRun_LiveCH(t *testing.T) {
	const runId = "run-events-mine"
	s, cleanup := newLiveStampedStore(t, runId)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second)

	// Another run's lifecycle row: named, and not ours.
	_, err := s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: "some-other-run", AppId: "github.com/example/play", TileKey: 1,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0.Add(time.Second),
	})
	require.NoError(t, err)
	// Ours, named.
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 3,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0.Add(2 * time.Second),
	})
	require.NoError(t, err)

	rows, err := s.ListRunEvents(factsstore.RunEventFilter{RunId: runId, Since: t0})
	require.NoError(t, err)
	require.Len(t, rows, 1, "another run's named row must not be in this run's trail")
	assert.Equal(t, uint64(3), rows[0].InstanceKey)

	// With no Since, only exactly-attributed rows are eligible at all.
	strict, err := s.ListRunEvents(factsstore.RunEventFilter{RunId: runId})
	require.NoError(t, err)
	require.Len(t, strict, 1)
}

// TestStore_ListRunEvents_RejectsEmptyRunId pins the guard: the view is one
// run's history, and an unfiltered read of an append-only table has no bound.
func TestStore_ListRunEvents_RejectsEmptyRunId(t *testing.T) {
	s, err := chstore.New(chstore.Defaults())
	require.NoError(t, err)
	_, err = s.ListRunEvents(factsstore.RunEventFilter{})
	require.Error(t, err)
}
