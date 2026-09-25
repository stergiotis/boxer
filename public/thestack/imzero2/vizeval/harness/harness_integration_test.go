//go:build integration

package harness

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScoreHostMetrics scores one committed scenario end to end with the
// default candidates (ADR-0257 Verification plan): the dataset runs, every
// admitted sink renders and is measured, and the card table of eight hosts —
// small enough to fit — has no overlapping or clipped text.
func TestScoreHostMetrics(t *testing.T) {
	if unmet, err := scene.CheckRequire(scene.RequireClickHouse); err != nil || unmet != "" {
		t.Skip("needs ClickHouse: ", unmet, err)
	}
	host, root := scenetest.BuildHost(t, scenetest.Host{})
	sc, err := vizeval.ReadScenario(filepath.Join(root, "apps", "play", "vizeval", "10_host_metrics"+vizeval.ScenarioSuffix))
	require.NoError(t, err)
	cands, err := DefaultCandidates(sc)
	require.NoError(t, err)
	cards, answers, err := Score(sc, cands, Options{
		OutDir: t.TempDir(), RepoRoot: root, HostBinary: host, Timeout: 90 * time.Second,
		Logger: zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel),
	})
	require.NoError(t, err)
	require.Len(t, answers, len(sc.Spec.Questions))
	require.Len(t, answers[0].Rows, 1, "busiest-cpu is a single host")
	for _, c := range cards {
		if c.Status == StatusFailed && strings.HasPrefix(c.Reason, string(scene.StatusSkip)) {
			t.Skip("scene skipped: ", c.Reason)
		}
		require.NotEqual(t, StatusFailed, c.Status, "%s: %s", c.Candidate.Sink, c.Reason)
		assert.Equal(t, int64(8), c.Rows)
		assert.Positive(t, c.Metrics[geometry.MetricTextRuns], c.Candidate.Sink)
	}
	assert.Equal(t, StatusScored, cards[0].Status, "the card table passes both gates")
}
