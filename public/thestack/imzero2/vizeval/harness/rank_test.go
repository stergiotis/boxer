package harness

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/judge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preferLarger prefers the image whose first byte is larger, on every
// criterion, in either order.
type preferLarger struct{ calls int }

func (inst *preferLarger) Complete(_ context.Context, r llm.Request) (llm.Response, error) {
	inst.calls++
	imgs := r.Messages[1].Images
	better := "2"
	if imgs[0].Data[0] > imgs[1].Data[0] {
		better = "1"
	}
	out := map[string]map[string]string{}
	for _, c := range judge.Criteria {
		out[c.Name] = map[string]string{"better": better, "why": "w"}
	}
	b, _ := json.Marshal(out)
	return llm.Response{Content: string(b)}, nil
}

func TestRankOrdersScoredCandidatesOfOneBatch(t *testing.T) {
	out := t.TempDir()
	sc, err := vizeval.ParseScenario("x/s"+vizeval.ScenarioSuffix, []byte(scenarioSrc))
	require.NoError(t, err)
	var lines []byte
	add := func(sink string, opts map[string]any, status StatusE, digest string, drawing string, pix byte, at string) {
		cand, e := vizeval.NewCandidate(sink, opts)
		require.NoError(t, e)
		c := Scorecard{Scenario: sc.Name, Candidate: cand, CandidateID: cand.ID(), Status: status, BatchDigest: digest,
			Dir: filepath.Join(sc.Name, cand.ID()), DrawingDigest: drawing, At: at,
			Metrics: map[string]float64{MetricTaskAccuracy: float64(pix) / 10}}
		require.NoError(t, os.MkdirAll(filepath.Join(out, c.Dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(out, c.Dir, "artifact.png"), []byte{pix}, 0o644))
		b, e := json.Marshal(c)
		require.NoError(t, e)
		lines = append(append(lines, b...), '\n')
	}
	add(vizeval.SinkCard, map[string]any{"palette": "magma"}, StatusScored, "d1", "draw-a", 1, "2026-09-25T10:00:00Z")
	add(vizeval.SinkCard, map[string]any{"palette": "plasma"}, StatusScored, "d1", "draw-a", 1, "2026-09-25T10:00:01Z")
	add(vizeval.SinkUnicode, nil, StatusScored, "d1", "draw-b", 3, "2026-09-25T10:00:02Z")
	add(vizeval.SinkJSON, nil, StatusScored, "d1", "draw-c", 2, "2026-09-25T10:00:03Z")
	add(vizeval.SinkUnicode, map[string]any{"width": 96}, StatusGated, "d1", "draw-d", 9, "2026-09-25T10:00:04Z")
	add(vizeval.SinkUnicode, map[string]any{"width": 97}, StatusScored, "d0", "draw-e", 9, "2026-09-25T09:00:00Z")
	require.NoError(t, os.WriteFile(filepath.Join(out, "scorecards.jsonl"), lines, 0o644))

	m := &preferLarger{}
	r, err := Rank(context.Background(), sc, RankOptions{OutDir: out, Judge: &judge.Judge{Client: m, Model: "m"}})
	require.NoError(t, err)
	require.Len(t, r.Candidates, 4, "the gated card and the one over other data are not ranked")
	assert.Len(t, r.Skipped, 1, "the other-data card is reported; a gated card is simply not rankable")
	assert.Equal(t, vizeval.SinkUnicode, r.Candidates[0].Candidate.Sink)
	assert.Equal(t, vizeval.SinkJSON, r.Candidates[1].Candidate.Sink)
	assert.InDelta(t, r.Candidates[2].Strength, r.Candidates[3].Strength, 1e-9, "one drawing, one strength")
	assert.Equal(t, 2*5, m.calls, "six pairs, one of them the same drawing: five compared, each in two orders")
	require.NotNil(t, r.Candidates[0].Accuracy)
	assert.FileExists(t, filepath.Join(out, sc.Name, "ranking.md"))
	assert.FileExists(t, filepath.Join(out, sc.Name, "ranking.json"))

	id1, _ := JudgementKey(sc.Name, judge.PairPromptVersion, "m", "draw-a", "draw-b")
	id2, _ := JudgementKey(sc.Name, judge.PairPromptVersion, "m2", "draw-a", "draw-b")
	assert.NotEqual(t, id1, id2, "another model's judgement is another measurement")
}
