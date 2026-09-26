package harness

import (
	"encoding/json/v2"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/judge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const judgeScenarioSrc = "---\nvizeval:\n  size: 1600x1000\n  intent: \"Spot the busiest host.\"\n  sinks: [card, unicode]\n" +
	"  questions:\n    - {id: busiest, prompt: \"Which host is busiest?\", answer: \"SELECT 1\"}\n" +
	"    - {id: count, prompt: \"How many hosts?\", answer: \"SELECT 1\"}\n" +
	"  gates:\n    task.accuracy: {min: 1}\n---\n\n# x\n\n" +
	"```sql base\nSELECT 1 AS v\n```\n\n```sql\nSELECT v FROM base\n```\n"

var judgeAnswers = []Answer{
	{ID: "busiest", Prompt: "Which host is busiest?", Rows: [][]string{{"host-07"}}},
	{ID: "count", Prompt: "How many hosts?", Rows: [][]string{{"8"}}},
}

// judgeFixture scores three planted candidates: two with distinct drawings
// and a third drawing what the first drew.
func judgeFixture(t *testing.T) (sc *vizeval.Scenario, out string, dir string, cards []Scorecard) {
	sc, err := vizeval.ParseScenario("x/j"+vizeval.ScenarioSuffix, []byte(judgeScenarioSrc))
	require.NoError(t, err)
	out = t.TempDir()
	dir = filepath.Join(out, sc.Name)
	for i, w := range []int64{60, 80, 100} {
		cand, err := vizeval.NewCandidate(vizeval.SinkUnicode, map[string]any{"width": w})
		require.NoError(t, err)
		c := Scorecard{Scenario: sc.Name, Candidate: cand, CandidateID: cand.ID(), Status: StatusScored,
			Dir: filepath.Join(sc.Name, cand.ID()), DrawingDigest: []string{"d1", "d2", "d1"}[i],
			Metrics: map[string]float64{}, Gates: map[string]bool{}}
		require.NoError(t, os.MkdirAll(filepath.Join(out, c.Dir), 0o755))
		img := image.NewRGBA(image.Rect(0, 0, 200, 100))
		for x := range 200 {
			img.Set(x, 50, color.RGBA{255, 255, 255, 255})
		}
		require.NoError(t, writePNG(filepath.Join(out, c.Dir, "artifact.png"), img))
		cards = append(cards, c)
	}
	return sc, out, dir, cards
}

func TestJudgeSheets(t *testing.T) {
	sc, out, dir, cards := judgeFixture(t)
	paths, err := writeJudgeSheets(out, dir, sc, judgeAnswers, cards)
	require.NoError(t, err)
	require.Len(t, paths, 3, "one sheet per drawing, and the control")
	for _, p := range paths {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		md := string(b)
		assert.Contains(t, md, "Which host is busiest?")
		assert.Contains(t, md, "Spot the busiest host.")
		for _, leak := range []string{"host-07", cards[0].CandidateID, "unicode", "SELECT", "d1"} {
			assert.NotContains(t, md, leak, "a sheet carries nothing but picture, intent and questions")
		}
	}
	key, err := readSheetKey(filepath.Join(dir, JudgeDirName))
	require.NoError(t, err)
	require.Len(t, key.Sheets, 3)
	shared := SheetID(sc.Name, "candidate", "d1")
	for _, s := range key.Sheets {
		if s.Sheet == shared {
			assert.ElementsMatch(t, []string{cards[0].CandidateID, cards[2].CandidateID}, s.Candidates, "one drawing, one sheet")
		}
	}
	again, err := writeJudgeSheets(out, dir, sc, judgeAnswers, cards)
	require.NoError(t, err)
	assert.Equal(t, paths, again, "sheet ids are stable across runs")
}

func reply(sheet, q, answer, reader string) judge.ReaderReply {
	return judge.ReaderReply{Sheet: sheet, Question: q, Answer: []string{answer}, Reader: reader}
}

func TestReaderRepliesBlindAndInformed(t *testing.T) {
	sc, out, dir, cards := judgeFixture(t)
	_, err := writeJudgeSheets(out, dir, sc, judgeAnswers, cards)
	require.NoError(t, err)
	s1, s2 := SheetID(sc.Name, "candidate", "d1"), SheetID(sc.Name, "candidate", "d2")
	control := SheetID(sc.Name, "control", "d1")

	// One blind reader per sheet; the control's reader cannot answer.
	rs := []judge.ReaderReply{
		reply(s1, "busiest", "host-07", "agent:a"), reply(s1, "count", "8", "agent:a"),
		reply(s2, "busiest", "host-03", "agent:b"), reply(s2, "count", "8", "agent:b"),
		{Sheet: control, Question: "busiest", Unreadable: true, Reader: "agent:c"},
		{Sheet: control, Question: "count", Unreadable: true, Reader: "agent:c"},
	}
	require.NoError(t, applyReaderReplies(sc, dir, judgeAnswers, rs, cards))
	assert.Equal(t, 1.0, cards[0].Metrics[MetricTaskAccuracy])
	assert.Equal(t, "reader:agent:a", cards[0].TaskJudge)
	assert.Equal(t, 1.0, cards[2].Metrics[MetricTaskAccuracy], "the candidate that drew the same picture shares the sheet")
	assert.Equal(t, 0.5, cards[1].Metrics[MetricTaskAccuracy])
	assert.Equal(t, StatusGated, cards[1].Status, "a task gate applies to a blind reader")

	// A reader that answered the control is informed, and so is one that
	// answered two sheets; neither lands in task.accuracy.
	sc, out, dir, cards = judgeFixture(t)
	_, err = writeJudgeSheets(out, dir, sc, judgeAnswers, cards)
	require.NoError(t, err)
	rs = []judge.ReaderReply{
		reply(s1, "busiest", "host-07", "agent:a"), reply(s1, "count", "8", "agent:a"),
		reply(control, "count", "8", "agent:a2"), reply(s2, "busiest", "host-07", "agent:a2"), reply(s2, "count", "8", "agent:a2"),
	}
	require.NoError(t, applyReaderReplies(sc, dir, judgeAnswers, rs, cards))
	assert.Equal(t, 1.0, cards[0].Metrics[MetricTaskAccuracy], "agent:a answered one sheet and no control")
	_, has := cards[1].Metrics[MetricTaskAccuracy]
	assert.False(t, has, "an informed reader's replies are not accuracy")
	assert.Equal(t, 1.0, cards[1].Metrics[MetricTaskInformedAccuracy])
	assert.Equal(t, StatusScored, cards[1].Status, "and gate nothing")
}

func TestReaderRepliesRefusals(t *testing.T) {
	sc, out, dir, cards := judgeFixture(t)
	_, err := writeJudgeSheets(out, dir, sc, judgeAnswers, cards)
	require.NoError(t, err)
	s1 := SheetID(sc.Name, "candidate", "d1")
	for name, rs := range map[string][]judge.ReaderReply{
		"two readers":      {reply(s1, "busiest", "x", "agent:a"), reply(s1, "count", "8", "agent:b")},
		"answered twice":   {reply(s1, "busiest", "x", "agent:a"), reply(s1, "busiest", "y", "agent:a")},
		"unknown question": {reply(s1, "nope", "x", "agent:a")},
	} {
		assert.Error(t, applyReaderReplies(sc, dir, judgeAnswers, rs, cards), name)
	}
	rs := []judge.ReaderReply{reply(s1, "busiest", "host-07", "agent:a"), reply("0000000000", "busiest", "x", "agent:z")}
	require.NoError(t, applyReaderReplies(sc, dir, judgeAnswers, rs, cards), "a sheet of another scenario is left alone")
	assert.Equal(t, 1.0, cards[0].Metrics[MetricTaskErrors], "a question without a reply is an error")
	_, has := cards[0].Metrics[MetricTaskAccuracy]
	assert.False(t, has)

	// The candidate now draws something else: its old sheet is stale.
	sc, out, dir, cards = judgeFixture(t)
	_, err = writeJudgeSheets(out, dir, sc, judgeAnswers, cards)
	require.NoError(t, err)
	cards[1].DrawingDigest = "d9"
	s2 := SheetID(sc.Name, "candidate", "d2")
	require.NoError(t, applyReaderReplies(sc, dir, judgeAnswers, []judge.ReaderReply{reply(s2, "busiest", "x", "agent:b")}, cards))
	require.Len(t, cards[1].Verdicts, 1)
	assert.True(t, strings.HasPrefix(cards[1].Verdicts[0].Error, "stale"))
}

func TestReadReplies(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	b, err := json.Marshal(reply("s", "q", "x", "agent:a"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, append(append([]byte("# a comment\n"), b...), '\n'), 0o644))
	rs, err := judge.ReadReplies(p)
	require.NoError(t, err)
	require.Len(t, rs, 1)
	require.NoError(t, os.WriteFile(p, []byte(`{"sheet":"s","question":"q","answer":[]}`+"\n"), 0o644))
	_, err = judge.ReadReplies(p)
	assert.Error(t, err, "a reply without a reader cannot be attributed")
}
