//go:build integration

package harness

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/geometry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/judge"
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

// TestFactsRoundTrip files a scorecard in boxer.facts and reads it back by
// its measurement key, the lookup that lets a search skip a candidate it has
// already scored (ADR-0257 §SD8).
func TestFactsRoundTrip(t *testing.T) {
	if unmet, err := scene.CheckRequire(scene.RequireClickHouse); err != nil || unmet != "" {
		t.Skip("needs ClickHouse: ", unmet, err)
	}
	ctx := t.Context()
	store, err := OpenFacts(ctx)
	require.NoError(t, err)
	defer store.Close()
	cand, err := vizeval.NewCandidate(vizeval.SinkCard, nil)
	require.NoError(t, err)
	card := Scorecard{
		Scenario: "facts_round_trip", Candidate: cand, CandidateID: cand.ID(),
		Build: "test" + strings.ReplaceAll(time.Now().UTC().Format("150405.000000"), ".", ""), BatchDigest: "d",
		Rows: 3, Status: StatusScored, Metrics: map[string]float64{geometry.MetricTextRuns: 7},
		Gates: map[string]bool{geometry.MetricTextClipped: true}, At: time.Now().UTC().Format(time.RFC3339),
	}
	_, found, err := lookupStored(ctx, store, card)
	require.NoError(t, err)
	require.False(t, found, "a fresh build has nothing stored")
	require.NoError(t, writeFacts(ctx, store, []Scorecard{card}))
	stored, found, err := lookupStored(ctx, store, card)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, card, stored)
}

// TestJudgementFactsLand files a ranking's comparisons and finds them by scan.
func TestJudgementFactsLand(t *testing.T) {
	if unmet, err := scene.CheckRequire(scene.RequireClickHouse); err != nil || unmet != "" {
		t.Skip("needs ClickHouse: ", unmet, err)
	}
	ctx := t.Context()
	store, err := OpenFacts(ctx)
	require.NoError(t, err)
	defer store.Close()
	scenario := "judgement_round_trip_" + strings.ReplaceAll(time.Now().UTC().Format("150405.000000"), ".", "")
	prefer := map[string]string{}
	for _, c := range judge.Criteria {
		prefer[c.Name] = judge.PreferA
	}
	r := Ranking{Scenario: scenario, BatchDigest: "d", Model: "m", Prompt: judge.PairPromptVersion,
		Verdicts: []judge.PairVerdict{{A: "ca", B: "cb", Prefer: prefer}}}
	require.NoError(t, writeJudgements(ctx, store, r, []judge.Picture{{ID: "ca", Drawing: "da"}, {ID: "cb", Drawing: "db"}}))
	var found bool
	for ent, e := range store.ScanVizevalJudgement(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, e)
		if ent.VizevalJudgement.Has && ent.VizevalJudgement.Val.Scenario == scenario {
			found = true
			assert.Equal(t, "da", ent.VizevalJudgement.Val.DrawingA)
			assert.Len(t, ent.VizevalJudgement.Val.Preference, len(judge.Criteria))
		}
	}
	assert.True(t, found)
}
