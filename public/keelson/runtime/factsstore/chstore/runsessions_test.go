//go:build llm_generated_opus47

package chstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
)

func TestStore_LookupRunStart_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	want := factsstore.RuntimeStartRow{
		RunId:        "abcdef1234567890",
		Hostname:     "test-host",
		Pid:          4242,
		GoVersion:    "go1.24.0",
		VcsRevision:  "abc123def4",
		VcsModified:  true,
		VcsBuildInfo: "vcs.time=2026-05-13T00:00:00Z",
		ModulePath:   "github.com/example/pebble2impl",
		Ts:           time.Now().UTC().Truncate(time.Second),
	}
	_, err := s.WriteRuntimeStart(want)
	require.NoError(t, err)

	got, found, err := s.LookupRunStart(context.Background(), want.RunId)
	require.NoError(t, err)
	require.True(t, found, "the just-written run should be looked up")
	assert.Equal(t, want.Hostname, got.Hostname)
	assert.Equal(t, want.Pid, got.Pid)
	assert.Equal(t, want.GoVersion, got.GoVersion)
	assert.Equal(t, want.VcsRevision, got.VcsRevision)
	assert.Equal(t, want.VcsModified, got.VcsModified)
	assert.Equal(t, want.VcsBuildInfo, got.VcsBuildInfo)
	assert.Equal(t, want.ModulePath, got.ModulePath)
	assert.WithinDuration(t, want.Ts, got.Ts, time.Second)
}

func TestStore_LookupRunStart_MissingRun_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, found, err := s.LookupRunStart(context.Background(), "no-such-run")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestStore_LookupRunStart_RejectsEmptyRunId_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, _, err := s.LookupRunStart(context.Background(), "")
	require.Error(t, err)
}

func TestStore_LifecyclesByRun_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	const runId = "run-roundtrip-001"
	// Two opens (different tile keys) + one close, all in this run, plus
	// an unrelated row in a different run that must NOT come back.
	t0 := time.Now().UTC().Truncate(time.Second)
	_, err := s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 1,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 2,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0.Add(time.Second),
	})
	require.NoError(t, err)
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 1,
		Phase: factsstore.AppLifecyclePhaseStopped, StopReason: "user-close",
		Ts: t0.Add(2 * time.Second),
	})
	require.NoError(t, err)
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: "other-run", AppId: "github.com/example/play", TileKey: 99,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0,
	})
	require.NoError(t, err)

	rows, err := s.LifecyclesByRun(context.Background(), chstore.LifecycleFilter{RunId: runId})
	require.NoError(t, err)
	require.Len(t, rows, 3, "exactly the three rows attributed to runId must come back")

	// Chronological order: started(tk=1) → started(tk=2) → stopped(tk=1).
	assert.Equal(t, factsstore.AppLifecyclePhaseStarted, rows[0].Phase)
	assert.EqualValues(t, 1, rows[0].TileKey)
	assert.Equal(t, factsstore.AppLifecyclePhaseStarted, rows[1].Phase)
	assert.EqualValues(t, 2, rows[1].TileKey)
	assert.Equal(t, factsstore.AppLifecyclePhaseStopped, rows[2].Phase)
	assert.EqualValues(t, 1, rows[2].TileKey)
	assert.Equal(t, "user-close", rows[2].StopReason)
	for _, r := range rows {
		assert.Equal(t, runId, r.RunId)
		assert.Equal(t, "github.com/example/play", string(r.AppId))
	}
}

func TestStore_LifecyclesByRun_NarrowsByAppId_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	const runId = "run-narrow-001"
	t0 := time.Now().UTC().Truncate(time.Second)
	_, err := s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 11,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0,
	})
	require.NoError(t, err)
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/imztop", TileKey: 12,
		Phase: factsstore.AppLifecyclePhaseStarted, Ts: t0.Add(time.Second),
	})
	require.NoError(t, err)
	_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
		RunId: runId, AppId: "github.com/example/play", TileKey: 13,
		Phase: factsstore.AppLifecyclePhaseStopped, Ts: t0.Add(2 * time.Second),
	})
	require.NoError(t, err)

	rows, err := s.LifecyclesByRun(context.Background(), chstore.LifecycleFilter{
		RunId: runId, AppId: "github.com/example/play",
	})
	require.NoError(t, err)
	require.Len(t, rows, 2, "AppId filter must drop the imztop row")
	for _, r := range rows {
		assert.Equal(t, "github.com/example/play", string(r.AppId))
	}
}

func TestStore_LifecyclesByRun_RejectsEmptyRunId_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.LifecyclesByRun(context.Background(), chstore.LifecycleFilter{})
	require.Error(t, err)
}

func TestStore_LifecyclesByRun_EmptyResult_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	rows, err := s.LifecyclesByRun(context.Background(), chstore.LifecycleFilter{RunId: "no-such-run"})
	require.NoError(t, err)
	assert.NotNil(t, rows, "empty slice (not nil) is the documented contract")
	assert.Len(t, rows, 0)
}

func TestStore_WriteRuntimeHeartbeat_RejectsEmptyRunId_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{})
	require.Error(t, err)
}

func TestStore_LastHeartbeatForRun_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	const runId = "run-hb-001"
	t0 := time.Now().UTC().Truncate(time.Second)
	// Three ticks two seconds apart; the latest (t0+4s) must be the one
	// LastHeartbeatForRun returns.
	for i := 0; i < 3; i++ {
		_, err := s.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{
			RunId: runId,
			Ts:    t0.Add(time.Duration(i*2) * time.Second),
		})
		require.NoError(t, err)
	}
	// Unrelated run — must not bleed into the lookup.
	_, err := s.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{
		RunId: "other-run", Ts: t0.Add(10 * time.Second),
	})
	require.NoError(t, err)

	ts, found, err := s.LastHeartbeatForRun(context.Background(), runId)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, ts.Equal(t0.Add(4*time.Second)),
		"latest heartbeat for runId, got %s want %s", ts, t0.Add(4*time.Second))
}

func TestStore_LastHeartbeatForRun_MissingRun_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, found, err := s.LastHeartbeatForRun(context.Background(), "no-such-run")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestStore_LastHeartbeatForRun_RejectsEmptyRunId_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, _, err := s.LastHeartbeatForRun(context.Background(), "")
	require.Error(t, err)
}
