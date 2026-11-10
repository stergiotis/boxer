package chstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
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
