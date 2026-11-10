package chstore_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
)

func TestStore_WriteRuntimeHeartbeat_RejectsEmptyRunId_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{})
	require.Error(t, err)
}

// TestStore_AppLaunchStats_RoundTrip_LiveCH is ADR-0214 §SD7's store-side
// verification: the cross-run aggregate the run-anchored reads do not offer. Two runs, one app opened in both, and the read must see the pair.
func TestStore_AppLaunchStats_RoundTrip_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	rows := []factsstore.AppLifecycleRow{
		// Two opens of `play`, in two different runs — the case the run-anchored
		// reader cannot answer.
		{RunId: "run00000000000a", AppId: "github.com/x/play", TileKey: 1, Phase: factsstore.AppLifecyclePhaseStarted, Ts: now.Add(-2 * time.Hour)},
		{RunId: "run00000000000b", AppId: "github.com/x/play", TileKey: 1, Phase: factsstore.AppLifecyclePhaseStarted, Ts: now.Add(-1 * time.Hour)},
		// One open of `imztop`, plus a close that must NOT count as a launch.
		{RunId: "run00000000000b", AppId: "github.com/x/imztop", TileKey: 2, Phase: factsstore.AppLifecyclePhaseStarted, Ts: now.Add(-30 * time.Minute)},
		{RunId: "run00000000000b", AppId: "github.com/x/imztop", TileKey: 2, Phase: factsstore.AppLifecyclePhaseStopped, Ts: now.Add(-20 * time.Minute)},
	}
	for _, r := range rows {
		_, err := s.WriteAppLifecycle(r)
		require.NoError(t, err)
	}

	stats, err := s.AppLaunchStats(ctx, 336*time.Hour, 0)
	require.NoError(t, err)
	byId := map[string]factsstore.AppLaunchStat{}
	for _, st := range stats {
		byId[string(st.AppId)] = st
	}
	play, ok := byId["github.com/x/play"]
	require.True(t, ok, "an app opened in two runs must appear; got %+v", stats)
	assert.Equal(t, uint64(2), play.Opens, "both runs' opens are counted")

	imztop, ok := byId["github.com/x/imztop"]
	require.True(t, ok)
	assert.Equal(t, uint64(1), imztop.Opens, "the close is not a launch")

	assert.Greater(t, play.Score, 0.0)
	assert.Greater(t, imztop.LastTs.Unix(), play.LastTs.Unix(),
		"imztop was opened more recently, whatever the scores are")
}

// TestStore_AppLaunchStats_RejectsNonPositiveHalfLife: the decay knob belongs
// to the caller, so a meaningless value is an error rather than a silent
// substitution.
func TestStore_AppLaunchStats_RejectsNonPositiveHalfLife(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	_, err := s.AppLaunchStats(context.Background(), 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "halfLife")
}

// TestStore_AppLaunchStats_EmptyTrail_LiveCH: a fresh table has no launches,
// and the reader says so with an empty slice rather than an error.
func TestStore_AppLaunchStats_EmptyTrail_LiveCH(t *testing.T) {
	s, cleanup := newLiveStore(t)
	defer cleanup()
	stats, err := s.AppLaunchStats(context.Background(), 336*time.Hour, 0)
	require.NoError(t, err)
	assert.Empty(t, stats)
}

// rowText is the one row of the test table, every column, as ClickHouse
// prints it. Reading a written value back here asks only that it is in the
// row, not which membership carries it: the check a writer that drops a
// field fails, whatever reads the kind afterwards.
func rowText(t *testing.T) (text string) {
	t.Helper()
	body, err := readClient().Query(context.Background(),
		"SELECT * FROM runtime_chstore_test.facts FORMAT JSONEachRow")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	text = strings.TrimSpace(string(raw))
	require.Equal(t, 0, strings.Count(text, "\n"), "expected one row, got:\n%s", text)
	require.NotEmpty(t, text)
	return
}

// TestStore_EveryWrittenValueIsInTheRow_LiveCH covers the three run-session
// kinds on the write side. Each case writes one row whose every string and
// number is unmistakable and looks for each in the stored row.
//
// A bool and a timestamp have no unmistakable text and are not checked.
func TestStore_EveryWrittenValueIsInTheRow_LiveCH(t *testing.T) {
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cases := []struct {
		name   string
		write  func(s *chstore.Store) error
		values []string
	}{
		{
			name: "runtime start",
			write: func(s *chstore.Store) (err error) {
				_, err = s.WriteRuntimeStart(factsstore.RuntimeStartRow{
					RunId: "run-zq1", Hostname: "host-zq2", Pid: 4242421,
					GoVersion: "go-zq3", VcsRevision: "rev-zq4", VcsBuildInfo: "info-zq5",
					ModulePath: "mod-zq6", BuildId: "build-zq7",
					Driver: factsstore.DriverWatchbillJob, DriverJob: "job-zq8", DriverAttempt: 7770001,
					DriverJobKind: "kind-zq9", Task: "task-zq10", TaskEpoch: 8880002, Call: "call-zq11",
					Ts: ts,
				})
				return
			},
			values: []string{"run-zq1", "host-zq2", "4242421", "go-zq3", "rev-zq4", "info-zq5", "mod-zq6",
				"build-zq7", factsstore.DriverWatchbillJob, "job-zq8", "7770001", "kind-zq9", "task-zq10",
				"8880002", "call-zq11"},
		},
		{
			name: "heartbeat",
			write: func(s *chstore.Store) (err error) {
				_, err = s.WriteRuntimeHeartbeat(factsstore.HeartbeatRow{RunId: "run-zq12", Ts: ts})
				return
			},
			values: []string{"run-zq12"},
		},
		{
			name: "app lifecycle",
			write: func(s *chstore.Store) (err error) {
				_, err = s.WriteAppLifecycle(factsstore.AppLifecycleRow{
					RunId: "run-zq13", AppId: "app.zq14", TileKey: 9990003,
					Phase: factsstore.AppLifecyclePhaseStopped, StopReason: "reason-zq15", Ts: ts,
				})
				return
			},
			values: []string{"run-zq13", "app.zq14", "9990003", factsstore.AppLifecyclePhaseStopped.String(), "reason-zq15"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, cleanup := newLiveStore(t)
			defer cleanup()
			require.NoError(t, tc.write(s))
			text := rowText(t)
			for _, v := range tc.values {
				assert.Contains(t, text, v)
			}
		})
	}
}
