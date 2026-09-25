package harness

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scenarioSrc = "---\nvizeval:\n  size: 1600x1000\n  sinks: [card, unicode]\n  settleMs: 900\n" +
	"  questions:\n    - {id: q, prompt: p, answer: \"SELECT 1\"}\n---\n\n# x\n\n" +
	"```sql base\nSELECT 1 AS v\n```\n\n```sql\nSELECT v FROM base\n```\n"

func TestCandidateScene(t *testing.T) {
	sc, err := vizeval.ParseScenario("x/s"+vizeval.ScenarioSuffix, []byte(scenarioSrc))
	require.NoError(t, err)
	cands, err := DefaultCandidates(sc)
	require.NoError(t, err)
	require.Len(t, cands, 2, "one per admitted sink")

	doc := candidateScene(sc, cands[1])
	assert.Equal(t, "play", doc.Spec.Launch)
	assert.Equal(t, sc.DatasetSQL(), doc.SQL, "play runs the dataset under the base CTE")
	assert.Equal(t, "*=body", doc.Spec.Env["BOXER_PLAY_TAB_ZONES"])
	assert.Equal(t, "1568x940", doc.Spec.Env["BOXER_PLAY_WINDOW_SIZE"])
	var seed map[string]any
	require.NoError(t, json.Unmarshal([]byte(doc.Spec.Env["BOXER_PLAY_EXPERIMENTS"]), &seed))
	assert.Equal(t, "result", seed["source"])
	assert.Equal(t, vizeval.SinkUnicode, seed["sink"])
	last := doc.Steps[len(doc.Steps)-1]
	assert.Equal(t, "capture", last.Do)
	assert.ElementsMatch(t, []string{carrierclient.SidecarSVG, carrierclient.SidecarTree}, last.Sidecars)
	assert.Equal(t, 900, doc.Steps[1].SettleMs)
}

func TestScoreOneRefusesWithoutRendering(t *testing.T) {
	sc, err := vizeval.ParseScenario("x/s"+vizeval.ScenarioSuffix, []byte(scenarioSrc))
	require.NoError(t, err)
	json, err := vizeval.NewCandidate(vizeval.SinkJSON, nil)
	require.NoError(t, err)
	card := Scorecard{Candidate: json}
	scoreOne(sc, &card, Dataset{Rows: 1}, Options{})
	assert.Equal(t, StatusInadmissible, card.Status, "not admitted")

	uni, err := vizeval.NewCandidate(vizeval.SinkUnicode, nil)
	require.NoError(t, err)
	card = Scorecard{Candidate: uni}
	scoreOne(sc, &card, Dataset{Rows: 100}, Options{})
	assert.Equal(t, StatusInadmissible, card.Status, "over the row cap")
	assert.Contains(t, card.Reason, "cap")
}

func TestFindNode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.tree.jsonl")
	require.NoError(t, os.WriteFile(p, []byte(
		`{"id":18446744073709551615,"role":"button","name":"Run","x":1,"y":2,"w":3,"h":4}`+"\n"+
			`{"id":14819800449806132138,"role":"unknown","name":"experiments.artifact","x":42,"y":332,"w":1517,"h":573}`+"\n"), 0o644))
	r, err := findNode(p, ArtifactNode)
	require.NoError(t, err)
	assert.Equal(t, geometry.Rect{X0: 42, Y0: 332, X1: 1559, Y1: 905}, r)
	_, err = findNode(p, "missing")
	assert.Error(t, err)
}
