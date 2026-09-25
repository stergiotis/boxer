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

// newLiveRunStore is newLiveStore with a run identity, so the rows it
// writes are attributed to a run as a host's are (ADR-0191 §SD3).
func newLiveRunStore(t *testing.T, runId string) (s *chstore.Store, cleanup func()) {
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

// Sessions pair across runs: a closed one carries both times and its
// reason, an open one no stop, and rows older than Since leave a session
// with no start rather than dropping it.
func TestStore_AppRuns_PairsAcrossRuns_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	write := func(run string, id string, key uint64, phase factsstore.AppLifecyclePhaseE, reason string, at time.Time) {
		_, err := s.WriteAppLifecycle(factsstore.AppLifecycleRow{RunId: run, AppId: app.AppIdT("github.com/example/" + id),
			TileKey: key, Phase: phase, StopReason: reason, Ts: at})
		require.NoError(t, err)
	}
	write("run-a", "play", 1, factsstore.AppLifecyclePhaseStarted, "", t0)
	write("run-a", "play", 1, factsstore.AppLifecyclePhaseStopped, "user-close", t0.Add(time.Minute))
	write("run-b", "play", 1, factsstore.AppLifecyclePhaseStarted, "", t0.Add(2*time.Minute))
	write("run-b", "tally", 3, factsstore.AppLifecyclePhaseStopped, "shutdown", t0.Add(3*time.Minute))
	write("run-b", "tally", 3, factsstore.AppLifecyclePhaseStarted, "", t0.Add(-48*time.Hour))

	rows, err := s.AppRuns(context.Background(), factsstore.AppTrailFilter{Since: t0.Add(-time.Minute)})
	require.NoError(t, err)
	require.Len(t, rows, 3)

	assert.Equal(t, "github.com/example/tally", string(rows[0].AppId), "most recent first")
	assert.True(t, rows[0].StartedAt.IsZero(), "the start fell before Since")
	assert.Equal(t, "shutdown", rows[0].StopReason)

	assert.Equal(t, "run-b", rows[1].RunId)
	assert.Equal(t, t0.Add(2*time.Minute), rows[1].StartedAt)
	assert.True(t, rows[1].StoppedAt.IsZero(), "still open")

	assert.Equal(t, "run-a", rows[2].RunId)
	assert.EqualValues(t, 1, rows[2].InstanceKey)
	assert.Equal(t, t0, rows[2].StartedAt)
	assert.Equal(t, t0.Add(time.Minute), rows[2].StoppedAt)
	assert.Equal(t, "user-close", rows[2].StopReason)
}

// The tail is per app: a chatty app keeps its newest rows and cannot push a
// quiet one out, and each row carries its run and window.
func TestStore_AppLogTail_PerApp_LiveCH(t *testing.T) {
	s, cleanup := newLiveRunStore(t, "run-logs")
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := range 5 {
		_, err := s.WriteLog(factsstore.LogRow{AppId: "chatty", InstanceKey: 7, Level: "info",
			Message: string(rune('a' + i)), Caller: "c.go:1", Ts: t0.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	_, err := s.WriteLog(factsstore.LogRow{AppId: "quiet", Level: "error", Message: "only", Error: "boom",
		Stack: "never read", Ts: t0})
	require.NoError(t, err)

	rows, err := s.AppLogTail(context.Background(), factsstore.AppTrailFilter{}, 2)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	byApp := map[string][]factsstore.AppLogRow{}
	for _, r := range rows {
		byApp[string(r.AppId)] = append(byApp[string(r.AppId)], r)
	}
	require.Len(t, byApp["chatty"], 2)
	assert.Equal(t, "e", byApp["chatty"][0].Message, "newest first")
	assert.Equal(t, "d", byApp["chatty"][1].Message)
	assert.EqualValues(t, 7, byApp["chatty"][0].InstanceKey)
	assert.Equal(t, "run-logs", byApp["chatty"][0].RunId)
	require.Len(t, byApp["quiet"], 1)
	assert.Equal(t, "boom", byApp["quiet"][0].Error)

	_, err = s.AppLogTail(context.Background(), factsstore.AppTrailFilter{}, 0)
	assert.Error(t, err)
}

// Audit rows fold per (app, subject, result); the mean skips requests that
// recorded no latency.
func TestStore_AppAuditSummary_Folds_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i, lat := range []uint32{10, 20, 0} {
		_, err := s.WriteAudit(factsstore.AuditRow{AppId: "reader", Subject: "keelson.query.apps", Result: "ok",
			LatencyMs: lat, Ts: t0.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	_, err := s.WriteAudit(factsstore.AuditRow{AppId: "reader", Subject: "keelson.query.apps", Result: "denied", Ts: t0})
	require.NoError(t, err)

	rows, err := s.AppAuditSummary(context.Background(), factsstore.AppTrailFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	ok := rows[0]
	assert.Equal(t, "ok", ok.Result)
	assert.EqualValues(t, 3, ok.Requests)
	assert.InDelta(t, 15.0, ok.MeanLatencyMs, 1e-9)
	assert.EqualValues(t, 20, ok.MaxLatencyMs)
	assert.Equal(t, t0, ok.FirstAt)
	assert.Equal(t, t0.Add(2*time.Second), ok.LastAt)
	assert.Equal(t, "denied", rows[1].Result)
	assert.Zero(t, rows[1].MeanLatencyMs)
}
